package service

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"

	"github.com/RHM-GER/Mailmune/internal/domain"
	"github.com/RHM-GER/Mailmune/internal/mailbox/imaptest"
	"github.com/RHM-GER/Mailmune/internal/store"
)

// noMoveCaps simulates an IMAP4rev1 server without the MOVE extension.
func noMoveCaps() imap.CapSet {
	return imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapIdle: {}}
}

// accountFromServer builds an account config pointing at the test server
// without persisting it.
func accountFromServer(server *imaptest.Server, id string) domain.AccountConfig {
	host, portText, err := net.SplitHostPort(server.Addr)
	if err != nil {
		panic(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		panic(err)
	}
	return domain.AccountConfig{
		ID: id, Name: id, Host: host, Port: port, Username: imaptest.Username,
		SecretRef: "imap/" + id, InboxFolder: "INBOX", SentFolder: "Sent",
		SpamFolder: "AI_SPAM_FILTER", SafetyMode: domain.SafetySafe,
		Enabled: true, DryRun: false, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
}

// automationAccount upserts an account with dry run disabled, simulating a
// user who explicitly enabled automatic moves after the dry-run phase.
func automationAccount(t *testing.T, svc *Service, server *imaptest.Server, id string, mode domain.SafetyMode, dryRun bool) domain.AccountConfig {
	t.Helper()
	account := accountFromServer(server, id)
	account.SafetyMode = mode
	account.DryRun = dryRun
	account.Enabled = true
	if err := svc.store.UpsertAccount(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	if err := svc.secrets.Set(account.SecretRef, imaptest.Password); err != nil {
		t.Fatal(err)
	}
	return account
}

// seedDecision stores a pending decision matching a real message in INBOX.
func seedDecision(t *testing.T, svc *Service, accountID string, server *imaptest.Server, uid uint32, subject string) domain.MessageDecision {
	t.Helper()
	state := server.MailboxState("INBOX")
	decision := domain.MessageDecision{
		ID: "dec-" + subject, AccountID: accountID, UIDValidity: state.UIDValidity, UID: uid,
		MessageIDHash: "hash-" + subject, OriginFolder: "INBOX", CurrentFolder: "INBOX",
		From: "spam@example.com", Subject: subject, Score: 0.99, Status: domain.StatusPending,
		IdempotencyKey: "scan:" + accountID + ":" + subject, ReceivedAt: time.Now(), CreatedAt: time.Now(),
	}
	if err := svc.store.SaveDecision(context.Background(), decision); err != nil {
		t.Fatal(err)
	}
	return decision
}

func TestConfirmedSpamMovesWhenAutomationEnabled(t *testing.T) {
	server := imaptest.New(t, rev2Caps())
	svc, _ := newTestService(t, server)
	account := automationAccount(t, svc, server, "auto-1", domain.SafetySafe, false)
	uid := server.AddMessage("INBOX", "spam@example.com", "Spam", "body", time.Now())
	decision := seedDecision(t, svc, account.ID, server, uid, "spam1")

	if err := svc.Review(context.Background(), domain.ReviewRequest{DecisionIDs: []string{decision.ID}, Action: domain.ReviewConfirm, IdempotencyKey: "rev-auto-1"}); err != nil {
		t.Fatal(err)
	}
	if len(server.Snapshot("INBOX")) != 0 {
		t.Fatal("confirmed spam was not moved out of INBOX")
	}
	if len(server.Snapshot("AI_SPAM_FILTER")) != 1 {
		t.Fatal("confirmed spam did not arrive in AI_SPAM_FILTER")
	}
	// The move operation reached a terminal moved/confirmed state.
	ops, _ := svc.store.MoveOperationsByState(context.Background(), account.ID, []domain.MoveState{domain.MoveMoved, domain.MoveConfirmed}, 0)
	if len(ops) != 1 {
		t.Fatalf("expected one moved operation, got %d", len(ops))
	}
	// The decision now reflects the new folder.
	updated, _ := svc.store.DecisionsByIDs(context.Background(), []string{decision.ID})
	if len(updated) != 1 || updated[0].CurrentFolder != "AI_SPAM_FILTER" {
		t.Fatalf("decision folder not updated: %+v", updated)
	}
}

func TestDryRunNeverMoves(t *testing.T) {
	server := imaptest.New(t, rev2Caps())
	svc, _ := newTestService(t, server)
	account := automationAccount(t, svc, server, "dry-1", domain.SafetySafe, true)
	uid := server.AddMessage("INBOX", "spam@example.com", "Spam", "body", time.Now())
	decision := seedDecision(t, svc, account.ID, server, uid, "dryspam")

	if err := svc.Review(context.Background(), domain.ReviewRequest{DecisionIDs: []string{decision.ID}, Action: domain.ReviewConfirm, IdempotencyKey: "rev-dry-1"}); err != nil {
		t.Fatal(err)
	}
	if len(server.Snapshot("INBOX")) != 1 {
		t.Fatal("dry run must never move mail")
	}
	ops, _ := svc.store.MoveOperationsByState(context.Background(), account.ID, nil, 0)
	if len(ops) != 0 {
		t.Fatalf("dry run planned a move: %d", len(ops))
	}
}

func TestConfirmAllNeverAutoMoves(t *testing.T) {
	server := imaptest.New(t, rev2Caps())
	svc, _ := newTestService(t, server)
	account := automationAccount(t, svc, server, "confirm-1", domain.SafetyConfirmAll, false)
	uid := server.AddMessage("INBOX", "spam@example.com", "Spam", "body", time.Now())
	decision := seedDecision(t, svc, account.ID, server, uid, "confirmall")

	if err := svc.Review(context.Background(), domain.ReviewRequest{DecisionIDs: []string{decision.ID}, Action: domain.ReviewConfirm, IdempotencyKey: "rev-confirm-1"}); err != nil {
		t.Fatal(err)
	}
	if len(server.Snapshot("INBOX")) != 1 {
		t.Fatal("confirm-all mode must never auto-move")
	}
}

func TestRejectedRestoresMovedMessage(t *testing.T) {
	server := imaptest.New(t, rev2Caps())
	svc, _ := newTestService(t, server)
	account := automationAccount(t, svc, server, "restore-1", domain.SafetySafe, false)
	uid := server.AddMessage("INBOX", "ham@example.com", "Wichtig", "body", time.Now())
	decision := seedDecision(t, svc, account.ID, server, uid, "falsepos")

	// Confirm moves it to spam.
	if err := svc.Review(context.Background(), domain.ReviewRequest{DecisionIDs: []string{decision.ID}, Action: domain.ReviewConfirm, IdempotencyKey: "rev-r1"}); err != nil {
		t.Fatal(err)
	}
	if len(server.Snapshot("AI_SPAM_FILTER")) != 1 {
		t.Fatal("message not moved to spam on confirm")
	}

	// Reject (false positive) restores it to the origin folder.
	if err := svc.Review(context.Background(), domain.ReviewRequest{DecisionIDs: []string{decision.ID}, Action: domain.ReviewReject, IdempotencyKey: "rev-r2"}); err != nil {
		t.Fatal(err)
	}
	if len(server.Snapshot("INBOX")) != 1 {
		t.Fatalf("message not restored to INBOX: %+v", server.Snapshot("INBOX"))
	}
	if len(server.Snapshot("AI_SPAM_FILTER")) != 0 {
		t.Fatal("message still in spam folder after restore")
	}
}

func TestMoveIsIdempotentOnRepeatedReview(t *testing.T) {
	server := imaptest.New(t, rev2Caps())
	svc, _ := newTestService(t, server)
	account := automationAccount(t, svc, server, "idem-1", domain.SafetySafe, false)
	uid := server.AddMessage("INBOX", "spam@example.com", "Spam", "body", time.Now())
	decision := seedDecision(t, svc, account.ID, server, uid, "idemspam")

	// Two confirms with different idempotency keys must not double-move.
	for i, key := range []string{"rev-i1", "rev-i2"} {
		if err := svc.Review(context.Background(), domain.ReviewRequest{DecisionIDs: []string{decision.ID}, Action: domain.ReviewConfirm, IdempotencyKey: key}); err != nil {
			t.Fatalf("review %d: %v", i, err)
		}
	}
	if len(server.Snapshot("AI_SPAM_FILTER")) != 1 {
		t.Fatalf("double move detected: %d in spam folder", len(server.Snapshot("AI_SPAM_FILTER")))
	}
	ops, _ := svc.store.MoveOperationsByState(context.Background(), account.ID, nil, 0)
	if len(ops) != 1 {
		t.Fatalf("expected exactly one move operation, got %d", len(ops))
	}
}

func TestNoMoveSupportIsTerminal(t *testing.T) {
	server := imaptest.New(t, noMoveCaps())
	svc, _ := newTestService(t, server)
	account := automationAccount(t, svc, server, "nomove-1", domain.SafetySafe, false)
	uid := server.AddMessage("INBOX", "spam@example.com", "Spam", "body", time.Now())
	decision := seedDecision(t, svc, account.ID, server, uid, "nomovespam")

	if err := svc.Review(context.Background(), domain.ReviewRequest{DecisionIDs: []string{decision.ID}, Action: domain.ReviewConfirm, IdempotencyKey: "rev-nm1"}); err != nil {
		t.Fatal(err)
	}
	if len(server.Snapshot("INBOX")) != 1 {
		t.Fatal("message moved despite no MOVE support")
	}
	ops, _ := svc.store.MoveOperationsByState(context.Background(), account.ID, []domain.MoveState{domain.MoveFailedTerminal}, 0)
	if len(ops) != 1 {
		t.Fatalf("expected one terminal failure, got %d", len(ops))
	}
}

func TestStuckMoveIsRequeuedOnStartup(t *testing.T) {
	server := imaptest.New(t, rev2Caps())
	db, svc := newTestServiceWithDB(t, server)
	account := automationAccount(t, svc, server, "stuck-1", domain.SafetySafe, false)
	// Simulate a crash: a move left in 'moving' state.
	op := domain.MoveOperation{
		ID: moveID(account.ID, domain.MoveToSpam, "INBOX", 42, 1), AccountID: account.ID,
		Direction: domain.MoveToSpam, OriginFolder: "INBOX", OriginUID: 42, OriginUIDValidity: 1,
		TargetFolder: "AI_SPAM_FILTER",
	}
	if _, _, err := db.PlanMoveOperation(context.Background(), op); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SetMoveState(context.Background(), op.ID, []domain.MoveState{domain.MovePlanned}, domain.MoveMoving, 0, 0, ""); err != nil {
		t.Fatal(err)
	}
	count, err := db.RequeueStuckMoves(context.Background())
	if err != nil || count != 1 {
		t.Fatalf("requeue = %d err=%v", count, err)
	}
	requeued, _, _ := db.MoveOperation(context.Background(), op.ID)
	if requeued.State != domain.MoveFailedRetry {
		t.Fatalf("stuck move not requeued: %s", requeued.State)
	}
}

// newTestServiceWithDB exposes the store as well for direct state setup.
func newTestServiceWithDB(t *testing.T, server *imaptest.Server) (*store.SQLite, *Service) {
	t.Helper()
	svc, db := newTestService(t, server)
	return db, svc
}
