package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/RHM-GER/Mailmune/internal/classifier"
	"github.com/RHM-GER/Mailmune/internal/domain"
	"github.com/RHM-GER/Mailmune/internal/events"
	"github.com/RHM-GER/Mailmune/internal/learning"
	"github.com/RHM-GER/Mailmune/internal/mailbox"
	"github.com/RHM-GER/Mailmune/internal/provider"
	"github.com/RHM-GER/Mailmune/internal/secrets"
	"github.com/RHM-GER/Mailmune/internal/store"
)

// Scanner serializes scan runs per account. Runs are crash-safe: progress
// lives in the database, synchronization resumes from the persisted UID
// state, and decisions are deduplicated by idempotency keys.
type Scanner struct {
	store   *store.SQLite
	secrets secrets.Store
	mailbox *mailbox.Client
	rules   *classifier.Rules
	ollama  *provider.Ollama
	hub     *events.Hub

	mu     sync.Mutex
	active map[string]*activeRun

	// testGate, when set, is invoked for every message before
	// classification. Tests use it to pause a run deterministically.
	testGate func()
}

type activeRun struct {
	runID  string
	cancel context.CancelFunc
	done   chan struct{}
}

// ScanEvent is the payload of scan.* events on the hub.
type ScanEvent struct {
	Run        domain.ScanRun `json:"run"`
	Candidates int            `json:"candidates,omitempty"`
	Moved      int            `json:"moved,omitempty"`
	Warnings   []string       `json:"warnings,omitempty"`
}

// minLearningSamples is the minimum number of confirmed examples before the
// statistical learner may contribute evidence. Small models stay silent so
// they cannot push cases over thresholds prematurely.
const minLearningSamples = 20

func newScanner(db *store.SQLite, secretStore secrets.Store, client *mailbox.Client, rules *classifier.Rules, ollama *provider.Ollama, hub *events.Hub) *Scanner {
	return &Scanner{store: db, secrets: secretStore, mailbox: client, rules: rules, ollama: ollama, hub: hub, active: map[string]*activeRun{}}
}

// RecoverInterrupted flags runs that were still active when the agent last
// stopped. Called once at startup; the next run resumes from the persisted
// UID state instead of blindly continuing.
func (s *Scanner) RecoverInterrupted(ctx context.Context) (int64, error) {
	return s.store.MarkInterruptedRuns(ctx)
}

// StartScan begins a background scan for an account. Starting a scan that is
// already running returns the active run unchanged (idempotent). When resync
// is true, the stored UID state of the inbox folder is dropped first, so the
// whole mailbox is re-read (decisions stay deduplicated by idempotency keys).
func (s *Scanner) StartScan(ctx context.Context, accountID string, resync bool) (domain.ScanRun, error) {
	account, err := s.store.Account(ctx, accountID)
	if err != nil {
		return domain.ScanRun{}, fmt.Errorf("account not found: %w", err)
	}
	password, err := s.secrets.Get(account.SecretRef)
	if err != nil {
		if errors.Is(err, secrets.ErrNotFound) {
			return domain.ScanRun{}, errors.New("stored password is missing; save the account again")
		}
		return domain.ScanRun{}, err
	}
	if resync {
		if err := s.store.DeleteFolderSyncState(ctx, accountID, account.InboxFolder); err != nil {
			return domain.ScanRun{}, err
		}
	}

	s.mu.Lock()
	if entry, ok := s.active[accountID]; ok {
		s.mu.Unlock()
		run, found, err := s.store.ScanRun(ctx, entry.runID)
		if err != nil || !found {
			return domain.ScanRun{}, err
		}
		return run, nil
	}
	now := time.Now().UTC()
	run := domain.ScanRun{ID: uuid.NewString(), AccountID: accountID, Status: domain.ScanRunning, Folder: account.InboxFolder, StartedAt: now, UpdatedAt: now}
	if err := s.store.CreateScanRun(ctx, run); err != nil {
		s.mu.Unlock()
		return domain.ScanRun{}, err
	}
	runCtx, cancel := context.WithCancel(context.Background())
	entry := &activeRun{runID: run.ID, cancel: cancel, done: make(chan struct{})}
	s.active[accountID] = entry
	s.mu.Unlock()

	s.publish("scan.started", ScanEvent{Run: run})
	go s.execute(runCtx, entry.done, account, password, run)
	return run, nil
}

// CancelScan requests cancellation of the account's active run.
func (s *Scanner) CancelScan(ctx context.Context, accountID string) (domain.ScanRun, bool, error) {
	s.mu.Lock()
	entry, ok := s.active[accountID]
	s.mu.Unlock()
	if !ok {
		return domain.ScanRun{}, false, nil
	}
	entry.cancel()
	run, found, err := s.store.ScanRun(ctx, entry.runID)
	if err != nil || !found {
		return domain.ScanRun{}, false, err
	}
	return run, true, nil
}

// Wait blocks until the account's active run has finished. It returns false
// when no run is active. Intended for tests.
func (s *Scanner) Wait(accountID string, timeout time.Duration) bool {
	s.mu.Lock()
	entry, ok := s.active[accountID]
	s.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case <-entry.done:
		return true
	case <-time.After(timeout):
		return false
	}
}

func (s *Scanner) execute(ctx context.Context, done chan struct{}, account domain.AccountConfig, password string, run domain.ScanRun) {
	defer close(done)
	defer func() {
		s.mu.Lock()
		delete(s.active, account.ID)
		s.mu.Unlock()
	}()

	result := s.scanAccount(ctx, account, password, run)

	runRow, _, err := s.store.ScanRun(context.Background(), run.ID)
	if err != nil {
		runRow = run
	}
	s.publish("scan.finished", ScanEvent{Run: runRow, Candidates: result.candidates, Moved: result.moved, Warnings: result.warnings})
}

type scanResult struct {
	processed  int
	candidates int
	moved      int
	warnings   []string
}

// baselineVirtualMessages bounds how strongly an imported global baseline may
// influence scoring, regardless of its raw size. It is expressed as a virtual
// count of confirmed examples so a huge corpus never drowns out the user's own
// confirmed learning.
const baselineVirtualMessages = 150

// buildScorer merges the per-account learner with the optional global
// baseline. Either part may be absent; nil is returned when there is nothing
// to score with.
func (s *Scanner) buildScorer(ctx context.Context, accountID string) *learning.Model {
	accountModel, accountErr := s.store.LoadLearningModel(ctx, accountID)
	if accountErr != nil {
		accountModel = nil
	}
	baselineModel, _, baselineOK, baselineErr := s.store.LoadBaseline(ctx, "global")
	if baselineErr != nil || !baselineOK {
		baselineModel = nil
	}
	switch {
	case accountModel != nil && baselineModel != nil:
		return accountModel.Merged(baselineModel, baselineModel.VirtualWeight(baselineVirtualMessages))
	case accountModel != nil:
		return accountModel
	case baselineModel != nil:
		// A baseline alone is a prior; expose it at its bounded virtual size.
		return learning.NewModel().Merged(baselineModel, baselineModel.VirtualWeight(baselineVirtualMessages))
	default:
		return nil
	}
}

func (s *Scanner) scanAccount(ctx context.Context, account domain.AccountConfig, password string, run domain.ScanRun) scanResult {
	result := scanResult{}
	folder := account.InboxFolder

	prev, _, err := s.store.FolderSyncState(ctx, account.ID, folder)
	if err != nil {
		s.finish(run.ID, domain.ScanFailed, redactError(err))
		return result
	}

	progressCounter := 0
	opts := mailbox.SyncOptions{
		MaxMessages: mailbox.DefaultMaxMessages,
		FetchText:   true,
		OnProgress: func(processed, estimatedTotal int) {
			progressCounter++
			// Persist every step so crashes lose at most one message of
			// progress; publish slightly less often to keep the stream calm.
			_ = s.store.UpdateScanProgress(context.Background(), run.ID, processed, estimatedTotal)
			if progressCounter%5 == 0 || processed == estimatedTotal {
				if current, ok, _ := s.store.ScanRun(context.Background(), run.ID); ok {
					s.publish("scan.progress", ScanEvent{Run: current})
				}
			}
		},
	}
	// Note: the bounded 90-day first pass belongs to the profiling feature
	// (Phase 4). Regular scans read up to MaxMessages newest messages.

	// Load the statistical learner once per run: the per-account model from
	// confirmed reviews, merged with the optional global baseline imported
	// from an external corpus. The baseline acts as a bounded prior so the
	// user's own confirmed learning can still outweigh it. A missing or
	// broken model never blocks scanning.
	var scorer classifier.StatisticalScorer
	if combined := s.buildScorer(ctx, account.ID); combined != nil && combined.Trained() >= minLearningSamples {
		scorer = combined
	}

	handler := func(message domain.MessageFeatures, text string) error {
		if s.testGate != nil {
			s.testGate()
		}
		// Text-based rules need the bounded body text; it is discarded after
		// classification and never persisted.
		message.Text = text
		if message.URLCount == 0 {
			message.URLCount = classifier.CountURLs(text)
		}
		features := learning.ExtractFeatures(message.Subject, message.From, message.FromDomain, text)
		classification := s.rules.ClassifyWithFeatures(message, account.Profile, features, scorer)
		s.consultModel(ctx, account, message, &classification)
		action := classifier.Decide(account.SafetyMode, classification)
		if action == classifier.ActionIgnore {
			return nil
		}
		result.candidates++
		status := domain.StatusPending
		current := message.Folder
		if action == classifier.ActionMove && !account.DryRun {
			destinationUID, moveErr := s.mailbox.MoveAtomic(ctx, account, password, message.Folder, message.UID, account.SpamFolder)
			if moveErr != nil {
				result.warnings = append(result.warnings, fmt.Sprintf("UID %d nicht verschoben: %v", message.UID, redactError(moveErr)))
			} else {
				status = domain.StatusMoved
				current = account.SpamFolder
				result.moved++
				if destinationUID != 0 {
					message.UID = destinationUID
				}
			}
		}
		hash := sha256.Sum256([]byte(strings.ToLower(message.MessageID)))
		decision := domain.MessageDecision{
			ID: uuid.NewString(), AccountID: account.ID, UIDValidity: message.UIDValidity, UID: message.UID,
			MessageIDHash: hex.EncodeToString(hash[:]), OriginFolder: message.Folder, CurrentFolder: current,
			From: message.From, Subject: message.Subject, Score: classification.Score, Status: status,
			Evidence: classification.Evidence, ModelVersion: classification.ModelUsed,
			IdempotencyKey: fmt.Sprintf("scan:%s:%d:%d:%s", account.ID, message.UIDValidity, message.UID, message.Folder),
			ReceivedAt: message.ReceivedAt, CreatedAt: time.Now().UTC(),
		}
		if err := s.store.SaveDecision(context.Background(), decision); err != nil {
			return err
		}
		// Keep the compact feature vector so a confirmed review can train the
		// local model later. It is deleted after training or purged after 180
		// days; it never contains raw message text.
		if err := s.store.SaveDecisionFeatures(context.Background(), decision.ID, features); err != nil {
			return err
		}
		result.processed++
		return nil
	}

	outcome, syncErr := s.mailbox.SyncFolder(ctx, account, password, folder, stateOrNil(prev), opts, handler)

	// Persist sync state only when the run completed or was cancelled after
	// processing; on unexpected errors the unchanged state forces a retry,
	// and decision idempotency keys keep the retry duplicate-free.
	if outcome != nil && outcome.UIDValidity != 0 && (syncErr == nil || errors.Is(syncErr, context.Canceled)) {
		state := domain.FolderSyncState{AccountID: account.ID, Folder: folder, UIDValidity: outcome.UIDValidity, LastUID: outcome.LastUID, LastSyncAt: time.Now().UTC()}
		if err := s.store.SaveFolderSyncState(context.Background(), state); err != nil {
			s.finish(run.ID, domain.ScanFailed, redactError(err))
			return result
		}
	}

	switch {
	case syncErr == nil:
		account.LastScanAt = ptrTime(time.Now().UTC())
		account.UpdatedAt = time.Now().UTC()
		_ = s.store.UpsertAccount(context.Background(), account)
		s.finish(run.ID, domain.ScanCompleted, "")
	case errors.Is(syncErr, context.Canceled):
		s.finish(run.ID, domain.ScanCancelled, "")
	default:
		s.finish(run.ID, domain.ScanFailed, redactError(syncErr))
	}
	return result
}

// consultModel runs the optional local Ollama classification for ambiguous
// cases and merges a validated verdict as a single independent signal group.
func (s *Scanner) consultModel(ctx context.Context, account domain.AccountConfig, message domain.MessageFeatures, classification *domain.Classification) bool {
	if !account.OllamaValidated || account.OllamaModel == "" {
		return false
	}
	if classification.Score < 0.25 || classification.Score >= 0.98 {
		return false
	}
	verdict, err := s.ollama.Classify(ctx, account.OllamaModel, message, account.Profile)
	if err != nil {
		return false
	}
	classification.ModelUsed = account.OllamaModel
	classification.ModelValidated = true
	classification.Evidence = append(classification.Evidence, domain.Evidence{Group: "model", Code: "local_model_" + verdict.Class, Weight: verdict.Score, Summary: "Lokales validiertes Modell: " + verdict.Class})
	if verdict.Class == "spam" {
		classification.Score = classification.Score*0.7 + verdict.Score*0.3
		classification.IndependentGroups++
	}
	return true
}

func (s *Scanner) finish(runID string, status domain.ScanStatus, errMsg string) {
	_ = s.store.FinishScanRun(context.Background(), runID, status, errMsg)
}

func (s *Scanner) publish(typ string, event ScanEvent) {
	if s.hub == nil {
		return
	}
	s.hub.Publish(typ, event)
}

// publishMove announces a completed move/restore so the UI can update the
// decision's current folder without a full refresh.
func (s *Scanner) publishMove(accountID string, state domain.MoveState) {
	if s.hub == nil {
		return
	}
	s.hub.Publish("move.completed", map[string]string{"accountId": accountID, "state": string(state)})
}

func stateOrNil(state domain.FolderSyncState) *domain.FolderSyncState {
	if state.UIDValidity == 0 && state.LastUID == 0 {
		return nil
	}
	return &state
}

// redactError keeps error text short and free of obvious secrets before it
// reaches the database, events or API responses.
func redactError(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	for _, marker := range []string{"password=", "password:", "Authorization"} {
		if index := strings.Index(strings.ToLower(text), strings.ToLower(marker)); index >= 0 {
			text = text[:index] + "[entfernt]"
		}
	}
	if len(text) > 400 {
		text = text[:400]
	}
	return text
}

func ptrTime(value time.Time) *time.Time { return &value }
