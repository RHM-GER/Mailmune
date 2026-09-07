package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/RHM-GER/Mailmune/internal/classifier"
	"github.com/RHM-GER/Mailmune/internal/domain"
	"github.com/RHM-GER/Mailmune/internal/mailbox"
	"github.com/RHM-GER/Mailmune/internal/secrets"
)

// maxMoveAttempts bounds retries of a retryable move before it becomes
// terminal, so a persistently failing operation surfaces instead of looping.
const maxMoveAttempts = 3

// Mover drives the crash-safe move state machine. It never deletes mail: the
// only write operation is an atomic UID MOVE into the configured spam folder,
// and a restore uses the same guarded flow back to the origin folder.
type Mover struct {
	scanner *Scanner
}

func newMover(scanner *Scanner) *Mover { return &Mover{scanner: scanner} }

// moveID builds the idempotency key of a move operation. It is stable for a
// given (account, origin UID, origin validity, direction), so a repeated plan
// after a crash never queues a second physical move.
func moveID(accountID string, direction domain.MoveDirection, originFolder string, uid, validity uint32) string {
	return fmt.Sprintf("move:%s:%s:%s:%d:%d", accountID, direction, originFolder, uid, validity)
}

// AutomationAllowed reports whether an account may move mail automatically.
// New accounts are always dry-run; automation requires an explicit later
// change. Confirm-all never auto-moves.
func AutomationAllowed(account domain.AccountConfig) bool {
	return account.Enabled && !account.DryRun && account.SafetyMode != domain.SafetyConfirmAll
}

// AutoMoveEligible reports whether a classification may be moved without a
// human decision under the account's safety mode. An LLM signal alone never
// qualifies, and a strong trust signal always blocks automation.
func AutoMoveEligible(mode domain.SafetyMode, result domain.Classification) bool {
	if result.StrongTrustSignal || result.IndependentGroups < 2 {
		return false
	}
	return classifier.Decide(mode, result) == classifier.ActionMove
}

// ApplyReviewOutcome advances the move state machine after a human review.
// Confirming spam may move the message when automation is allowed; rejecting a
// moved message restores it to its origin folder. Both are idempotent.
func (m *Mover) ApplyReviewOutcome(ctx context.Context, account domain.AccountConfig, decision domain.MessageDecision) error {
	if !AutomationAllowed(account) {
		return nil // Dry run: record the review, never move.
	}
	switch decision.Status {
	case domain.StatusConfirmed:
		// A known correspondent or a low-confidence case is never auto-moved.
		if decision.CurrentFolder == account.SpamFolder {
			return nil // already in spam folder
		}
		return m.planAndExecute(ctx, account, decision, domain.MoveToSpam)
	case domain.StatusRejected:
		// False positive: restore only if it was actually moved before.
		if decision.OriginFolder == decision.CurrentFolder {
			return nil
		}
		return m.planRestore(ctx, account, decision)
	default:
		return nil
	}
}

func (m *Mover) planAndExecute(ctx context.Context, account domain.AccountConfig, decision domain.MessageDecision, direction domain.MoveDirection) error {
	op := domain.MoveOperation{
		ID:                moveID(account.ID, direction, decision.OriginFolder, decision.UID, decision.UIDValidity),
		AccountID:         account.ID,
		DecisionID:        decision.ID,
		Direction:         direction,
		OriginFolder:      decision.OriginFolder,
		OriginUID:         decision.UID,
		OriginUIDValidity: decision.UIDValidity,
		TargetFolder:      account.SpamFolder,
		MessageIDHash:     decision.MessageIDHash,
	}
	planned, _, err := m.scanner.store.PlanMoveOperation(ctx, op)
	if err != nil {
		return err
	}
	return m.execute(ctx, account, planned)
}

func (m *Mover) planRestore(ctx context.Context, account domain.AccountConfig, decision domain.MessageDecision) error {
	// Restore goes from the spam folder back to the stored origin folder. The
	// current UID in the spam folder is the destination UID recorded earlier.
	originOpID := moveID(account.ID, domain.MoveToSpam, decision.OriginFolder, decision.UID, decision.UIDValidity)
	origin, found, err := m.scanner.store.MoveOperation(ctx, originOpID)
	if err != nil {
		return err
	}
	currentUID := decision.UID
	currentValidity := decision.UIDValidity
	currentFolder := decision.CurrentFolder
	if found && origin.State == domain.MoveMoved && origin.DestUID != 0 {
		currentUID = origin.DestUID
		currentValidity = origin.DestUIDValidity
		currentFolder = account.SpamFolder
	}
	op := domain.MoveOperation{
		ID:                moveID(account.ID, domain.MoveRestore, currentFolder, currentUID, currentValidity),
		AccountID:         account.ID,
		DecisionID:        decision.ID,
		Direction:         domain.MoveRestore,
		OriginFolder:      currentFolder,
		OriginUID:         currentUID,
		OriginUIDValidity: currentValidity,
		TargetFolder:      decision.OriginFolder,
		MessageIDHash:     decision.MessageIDHash,
	}
	planned, _, err := m.scanner.store.PlanMoveOperation(ctx, op)
	if err != nil {
		return err
	}
	return m.execute(ctx, account, planned)
}

// execute drives one operation through the state machine. It is safe to call
// repeatedly: completed operations are no-ops, in-flight operations are
// reconciled by reading, and failures are retried up to a bound.
func (m *Mover) execute(ctx context.Context, account domain.AccountConfig, op domain.MoveOperation) error {
	switch op.State {
	case domain.MoveMoved, domain.MoveConfirmed, domain.MoveRestored:
		return nil // already done
	case domain.MoveFailedTerminal:
		return errors.New(op.LastError)
	}

	password, err := m.scanner.secrets.Get(account.SecretRef)
	if err != nil {
		if errors.Is(err, secrets.ErrNotFound) {
			return errors.New("stored password is missing; save the account again")
		}
		return err
	}

	// Reconcile a crash that happened after the physical move was issued.
	if op.State == domain.MoveMoving || op.State == domain.MoveRestoring || op.State == domain.MoveFailedRetry {
		if m.reconcile(ctx, account, password, op) {
			return nil
		}
	}

	// planned/failed_retryable -> moving (guarded; only one transition wins).
	inFlight := domain.MoveMoving
	if op.Direction == domain.MoveRestore {
		inFlight = domain.MoveRestoring
	}
	ok, err := m.scanner.store.SetMoveState(ctx, op.ID, []domain.MoveState{domain.MovePlanned, domain.MoveFailedRetry}, inFlight, 0, 0, "")
	if err != nil {
		return err
	}
	if !ok {
		// Another worker advanced it; reload and stop.
		return nil
	}

	done := domain.MoveMoved
	if op.Direction == domain.MoveRestore {
		done = domain.MoveRestored
	}
	result, err := m.scanner.mailbox.MoveVerified(ctx, account, password, op.OriginFolder, op.OriginUID, op.OriginUIDValidity, op.TargetFolder, true)
	if err != nil {
		return m.classifyFailure(ctx, op, err)
	}

	if result.Confirmed {
		if _, err := m.scanner.store.SetMoveState(ctx, op.ID, []domain.MoveState{inFlight}, done, result.DestUID, result.DestValidity, ""); err != nil {
			return err
		}
		m.updateDecisionFolder(ctx, op, result.DestUID)
		m.scanner.publishMove(op.AccountID, done)
		return nil
	}

	// Moved but the server did not return COPYUID: reconcile by reading.
	if m.reconcileUnconfirmed(ctx, account, password, op, done) {
		return nil
	}
	_, _ = m.scanner.store.SetMoveState(ctx, op.ID, []domain.MoveState{inFlight}, domain.MoveFailedRetry, 0, 0, "move not confirmed by server")
	return errors.New("move not confirmed by server")
}

// classifyFailure maps a guarded-move error onto the state machine. A missing
// message means the move already happened (reconcile); a UIDVALIDITY change or
// unsupported MOVE is terminal for this attempt and needs attention.
func (m *Mover) classifyFailure(ctx context.Context, op domain.MoveOperation, moveErr error) error {
	inFlight := domain.MoveMoving
	if op.Direction == domain.MoveRestore {
		inFlight = domain.MoveRestoring
	}
	switch {
	case errors.Is(moveErr, mailbox.ErrMessageGone):
		// The message left the origin folder: treat as moved and reconcile.
		if m.reconcileGone(ctx, op, inFlight) {
			return nil
		}
		_, _ = m.scanner.store.SetMoveState(ctx, op.ID, []domain.MoveState{inFlight}, domain.MoveFailedRetry, 0, 0, moveErr.Error())
		return moveErr
	case errors.Is(moveErr, mailbox.ErrUIDValidityChanged):
		_, _ = m.scanner.store.SetMoveState(ctx, op.ID, []domain.MoveState{inFlight}, domain.MoveFailedTerminal, 0, 0, "UIDVALIDITY changed; resync required")
		return moveErr
	case errors.Is(moveErr, mailbox.ErrMoveUnsupported), errors.Is(moveErr, mailbox.ErrFolderMissing):
		_, _ = m.scanner.store.SetMoveState(ctx, op.ID, []domain.MoveState{inFlight}, domain.MoveFailedTerminal, 0, 0, redactError(moveErr))
		return moveErr
	default:
		return m.retryOrFail(ctx, op, inFlight, moveErr)
	}
}

func (m *Mover) retryOrFail(ctx context.Context, op domain.MoveOperation, inFlight domain.MoveState, moveErr error) error {
	if op.Attempts+1 >= maxMoveAttempts {
		_, _ = m.scanner.store.SetMoveState(ctx, op.ID, []domain.MoveState{inFlight}, domain.MoveFailedTerminal, 0, 0, redactError(moveErr))
	} else {
		_, _ = m.scanner.store.SetMoveState(ctx, op.ID, []domain.MoveState{inFlight}, domain.MoveFailedRetry, 0, 0, redactError(moveErr))
	}
	return moveErr
}

// reconcile checks whether an interrupted move actually completed by reading
// the origin folder. If the origin UID is gone, the move happened.
func (m *Mover) reconcile(ctx context.Context, account domain.AccountConfig, password string, op domain.MoveOperation) bool {
	exists, validity, err := m.scanner.mailbox.MessageExists(ctx, account, password, op.OriginFolder, op.OriginUID)
	if err != nil {
		_ = err
		return false
	}
	if validity != op.OriginUIDValidity {
		// Folder rebuilt; UIDs are meaningless. Mark terminal for re-sync.
		_, _ = m.scanner.store.SetMoveState(ctx, op.ID, nil, domain.MoveFailedTerminal, 0, 0, "UIDVALIDITY changed during reconcile")
		return true
	}
	if !exists {
		done := domain.MoveMoved
		if op.Direction == domain.MoveRestore {
			done = domain.MoveRestored
		}
		_, _ = m.scanner.store.SetMoveState(ctx, op.ID, nil, done, op.DestUID, op.DestUIDValidity, "")
		m.updateDecisionFolder(ctx, op, op.DestUID)
		m.scanner.publishMove(op.AccountID, done)
		return true
	}
	// Still present: the move did not happen; allow a retry.
	_, _ = m.scanner.store.SetMoveState(ctx, op.ID, nil, domain.MoveFailedRetry, 0, 0, "")
	return false
}

func (m *Mover) reconcileGone(ctx context.Context, op domain.MoveOperation, inFlight domain.MoveState) bool {
	done := domain.MoveMoved
	if op.Direction == domain.MoveRestore {
		done = domain.MoveRestored
	}
	ok, err := m.scanner.store.SetMoveState(ctx, op.ID, []domain.MoveState{inFlight}, done, op.DestUID, op.DestUIDValidity, "")
	if err != nil {
		return false
	}
	if ok {
		m.updateDecisionFolder(ctx, op, op.DestUID)
		m.scanner.publishMove(op.AccountID, done)
	}
	return ok
}

func (m *Mover) reconcileUnconfirmed(ctx context.Context, account domain.AccountConfig, password string, op domain.MoveOperation, done domain.MoveState) bool {
	exists, _, err := m.scanner.mailbox.MessageExists(ctx, account, password, op.OriginFolder, op.OriginUID)
	if err != nil || exists {
		return false
	}
	ok, err := m.scanner.store.SetMoveState(ctx, op.ID, []domain.MoveState{domain.MoveMoving, domain.MoveRestoring}, done, op.DestUID, op.DestUIDValidity, "")
	if err != nil || !ok {
		return false
	}
	m.updateDecisionFolder(ctx, op, op.DestUID)
	m.scanner.publishMove(op.AccountID, done)
	return true
}

// updateDecisionFolder keeps the user-facing decision in sync with a completed
// move so the review list reflects the true current folder.
func (m *Mover) updateDecisionFolder(ctx context.Context, op domain.MoveOperation, destUID uint32) {
	if op.DecisionID == "" {
		return
	}
	folder := op.TargetFolder
	if op.Direction == domain.MoveRestore {
		folder = op.TargetFolder
	}
	_ = m.scanner.store.UpdateDecisionFolder(ctx, op.DecisionID, folder, destUID, time.Now().UTC())
}

// RecoverStuckMoves requeues in-flight operations on startup and reconciles
// them against the mailbox before any retry.
func (m *Mover) RecoverStuckMoves(ctx context.Context) (int64, error) {
	return m.scanner.store.RequeueStuckMoves(ctx)
}

// ProcessPending drives all retryable/planned operations of an account, e.g.
// after a reconnect. It is bounded and never blocks startup.
func (m *Mover) ProcessPending(ctx context.Context, account domain.AccountConfig) error {
	ops, err := m.scanner.store.MoveOperationsByState(ctx, account.ID, []domain.MoveState{domain.MovePlanned, domain.MoveFailedRetry}, 0)
	if err != nil {
		return err
	}
	for _, op := range ops {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := m.execute(ctx, account, op); err != nil {
			// One failing operation must not stop the others.
			continue
		}
	}
	return nil
}
