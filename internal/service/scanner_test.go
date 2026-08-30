package service

import (
	"context"
	"net"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"

	"github.com/RHM-GER/Mailmune/internal/domain"
	"github.com/RHM-GER/Mailmune/internal/learning"
	"github.com/RHM-GER/Mailmune/internal/mailbox"
	"github.com/RHM-GER/Mailmune/internal/mailbox/imaptest"
	"github.com/RHM-GER/Mailmune/internal/secrets"
	"github.com/RHM-GER/Mailmune/internal/store"
)

type memSecrets struct{ values map[string]string }

func (m *memSecrets) Set(reference, secret string) error { m.values[reference] = secret; return nil }
func (m *memSecrets) Get(reference string) (string, error) {
	if value, ok := m.values[reference]; ok {
		return value, nil
	}
	return "", secrets.ErrNotFound
}
func (m *memSecrets) Delete(reference string) error { delete(m.values, reference); return nil }

func rev2Caps() imap.CapSet {
	return imap.CapSet{imap.CapIMAP4rev2: {}, imap.CapMove: {}, imap.CapIdle: {}}
}

func newTestService(t *testing.T, server *imaptest.Server) (*Service, *store.SQLite) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	client := mailbox.NewClientWithRoots(server.Roots, 5*time.Second)
	return NewWithMailbox(db, &memSecrets{values: map[string]string{}}, client), db
}

func createTestAccount(t *testing.T, svc *Service, server *imaptest.Server, id string) domain.AccountConfig {
	t.Helper()
	host, portText, err := net.SplitHostPort(server.Addr)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	request := SaveAccountRequest{
		Account: domain.AccountConfig{
			ID: id, Name: id, Host: host, Port: port, Username: imaptest.Username,
			SafetyMode: domain.SafetySafe, Enabled: true, DryRun: false,
		},
		Password: imaptest.Password,
	}
	account, err := svc.SaveAccount(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	return account
}

func spamMessage(server *imaptest.Server, subject string, when time.Time) {
	server.AddMessage("INBOX", "spam@example.com", subject,
		"Konto gesperrt, sofort handeln!", when,
		"Authentication-Results: mx.test; spf=fail")
}

func waitForScan(t *testing.T, svc *Service, accountID string) domain.ScanRun {
	t.Helper()
	if !svc.scanner.Wait(accountID, 30*time.Second) {
		t.Fatal("scan did not finish in time")
	}
	runs, err := svc.ScanRuns(context.Background(), accountID, 1)
	if err != nil || len(runs) == 0 {
		t.Fatalf("no scan run recorded: %v", err)
	}
	return runs[0]
}

func TestNewAccountsAlwaysStartInDryRun(t *testing.T) {
	server := imaptest.New(t, rev2Caps())
	svc, _ := newTestService(t, server)
	account := createTestAccount(t, svc, server, "acc-dry")
	if !account.DryRun {
		t.Fatal("new account must start in dry run")
	}
	// A scan of an obvious spam candidate must not move anything.
	spamMessage(server, "Gewinn: Konto gesperrt, sofort handeln", time.Now())
	if _, err := svc.StartScan(context.Background(), account.ID); err != nil {
		t.Fatal(err)
	}
	run := waitForScan(t, svc, account.ID)
	if run.Status != domain.ScanCompleted {
		t.Fatalf("run status = %s (%s)", run.Status, run.Error)
	}
	if moved := server.Snapshot("AI_SPAM_FILTER"); len(moved) != 0 {
		t.Fatalf("dry run moved %d messages", len(moved))
	}
}

func TestScanLifecycleIdempotencyAndResume(t *testing.T) {
	server := imaptest.New(t, rev2Caps())
	svc, db := newTestService(t, server)
	account := createTestAccount(t, svc, server, "acc-1")
	now := time.Now()
	spamMessage(server, "Gewinn: Konto gesperrt, sofort handeln", now.Add(-time.Hour))
	spamMessage(server, "Zahlung fehlgeschlagen: sofort handeln", now.Add(-30*time.Minute))
	server.AddMessage("INBOX", "freund@example.com", "Hallo", "Viele Grüße", now)

	if _, err := svc.StartScan(context.Background(), account.ID); err != nil {
		t.Fatal(err)
	}
	run := waitForScan(t, svc, account.ID)
	if run.Status != domain.ScanCompleted || run.Processed < 2 {
		t.Fatalf("unexpected first run: %+v", run)
	}
	decisions, err := db.ListDecisions(context.Background(), store.DecisionFilter{AccountID: account.ID})
	if err != nil || len(decisions) != 2 {
		t.Fatalf("decisions = %d, want 2 (err=%v)", len(decisions), err)
	}
	state, ok, err := db.FolderSyncState(context.Background(), account.ID, "INBOX")
	if err != nil || !ok || state.LastUID != 3 {
		t.Fatalf("sync state not persisted: %+v ok=%v err=%v", state, ok, err)
	}

	// Second run without new messages: completes and adds nothing.
	if _, err := svc.StartScan(context.Background(), account.ID); err != nil {
		t.Fatal(err)
	}
	run = waitForScan(t, svc, account.ID)
	if run.Status != domain.ScanCompleted {
		t.Fatalf("second run status = %s (%s)", run.Status, run.Error)
	}
	decisions, _ = db.ListDecisions(context.Background(), store.DecisionFilter{AccountID: account.ID})
	if len(decisions) != 2 {
		t.Fatalf("decisions changed without new mail: %d", len(decisions))
	}

	// A new spam message is picked up incrementally by UID.
	spamMessage(server, "Lotterie gewonnen: sofort handeln", now)
	if _, err := svc.StartScan(context.Background(), account.ID); err != nil {
		t.Fatal(err)
	}
	run = waitForScan(t, svc, account.ID)
	if run.Status != domain.ScanCompleted || run.Processed != 1 {
		t.Fatalf("incremental run: %+v", run)
	}
	decisions, _ = db.ListDecisions(context.Background(), store.DecisionFilter{AccountID: account.ID})
	if len(decisions) != 3 {
		t.Fatalf("decisions = %d, want 3", len(decisions))
	}
}

func TestScanStartIsIdempotentWhileRunning(t *testing.T) {
	server := imaptest.New(t, rev2Caps())
	svc, _ := newTestService(t, server)
	account := createTestAccount(t, svc, server, "acc-gate")

	release := make(chan struct{})
	svc.scanner.testGate = func() { <-release }
	t.Cleanup(func() { svc.scanner.testGate = nil })
	spamMessage(server, "Gewinn: sofort handeln", time.Now())
	spamMessage(server, "Konto gesperrt: sofort handeln", time.Now())

	first, err := svc.StartScan(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.StartScan(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("second start created a new run: %s vs %s", first.ID, second.ID)
	}
	close(release)
	waitForScan(t, svc, account.ID)
}

func TestScanCancelAndResume(t *testing.T) {
	server := imaptest.New(t, rev2Caps())
	svc, db := newTestService(t, server)
	account := createTestAccount(t, svc, server, "acc-cancel")
	now := time.Now()
	for _, subject := range []string{"Gewinn: sofort handeln", "Konto gesperrt: sofort handeln", "Zahlung fehlgeschlagen"} {
		spamMessage(server, subject, now)
	}

	blocked := make(chan struct{})
	release := make(chan struct{})
	first := true
	svc.scanner.testGate = func() {
		if first {
			first = false
			close(blocked)
			<-release
		}
	}
	t.Cleanup(func() { svc.scanner.testGate = nil })

	if _, err := svc.StartScan(context.Background(), account.ID); err != nil {
		t.Fatal(err)
	}
	<-blocked
	if _, active, err := svc.CancelScan(context.Background(), account.ID); err != nil || !active {
		t.Fatalf("cancel failed: active=%v err=%v", active, err)
	}
	if svc.scanner.Wait(account.ID, 200*time.Millisecond) {
		t.Fatal("scan finished while the handler is blocked")
	}
	close(release)
	run := waitForScan(t, svc, account.ID)
	if run.Status != domain.ScanCancelled {
		t.Fatalf("run status = %s, want cancelled (%s)", run.Status, run.Error)
	}
	state, ok, err := db.FolderSyncState(context.Background(), account.ID, "INBOX")
	if err != nil || !ok || state.LastUID != 1 {
		t.Fatalf("partial sync state after cancel: %+v ok=%v err=%v", state, ok, err)
	}

	// Resume: the remaining UIDs are fetched, nothing is duplicated.
	if _, err := svc.StartScan(context.Background(), account.ID); err != nil {
		t.Fatal(err)
	}
	run = waitForScan(t, svc, account.ID)
	if run.Status != domain.ScanCompleted {
		t.Fatalf("resume status = %s (%s)", run.Status, run.Error)
	}
	decisions, _ := db.ListDecisions(context.Background(), store.DecisionFilter{AccountID: account.ID})
	if len(decisions) != 3 {
		t.Fatalf("decisions = %d, want 3 after resume", len(decisions))
	}
	state, _, _ = db.FolderSyncState(context.Background(), account.ID, "INBOX")
	if state.LastUID != 3 {
		t.Fatalf("last UID after resume = %d, want 3", state.LastUID)
	}
}

func TestReviewTrainsLearnerFromConfirmedDecisions(t *testing.T) {
	server := imaptest.New(t, rev2Caps())
	svc, db := newTestService(t, server)
	account := createTestAccount(t, svc, server, "acc-learn")

	spamDecision := domain.MessageDecision{
		ID: "learn-spam", AccountID: account.ID, UIDValidity: 1, UID: 10, MessageIDHash: "h1",
		OriginFolder: "INBOX", CurrentFolder: "INBOX", From: "spam@lotterie.example", Subject: "Gewinn",
		Score: 0.8, Status: domain.StatusPending, IdempotencyKey: "learn:1",
		ReceivedAt: time.Now(), CreatedAt: time.Now(),
	}
	hamDecision := domain.MessageDecision{
		ID: "learn-ham", AccountID: account.ID, UIDValidity: 1, UID: 11, MessageIDHash: "h2",
		OriginFolder: "INBOX", CurrentFolder: "INBOX", From: "kollege@firma.example", Subject: "Meeting",
		Score: 0.62, Status: domain.StatusPending, IdempotencyKey: "learn:2",
		ReceivedAt: time.Now(), CreatedAt: time.Now(),
	}
	for _, decision := range []domain.MessageDecision{spamDecision, hamDecision} {
		if err := db.SaveDecision(context.Background(), decision); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.SaveDecisionFeatures(context.Background(), spamDecision.ID, map[string]int{"lotteriegewinn": 3, "dom:lotterie.example": 2}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveDecisionFeatures(context.Background(), hamDecision.ID, map[string]int{"projektbericht": 3, "dom:firma.example": 2}); err != nil {
		t.Fatal(err)
	}

	if err := svc.Review(context.Background(), domain.ReviewRequest{DecisionIDs: []string{spamDecision.ID}, Action: domain.ReviewConfirm, IdempotencyKey: "rev-1"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Review(context.Background(), domain.ReviewRequest{DecisionIDs: []string{hamDecision.ID}, Action: domain.ReviewReject, IdempotencyKey: "rev-2"}); err != nil {
		t.Fatal(err)
	}
	model, err := db.LoadLearningModel(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if model.SpamMessages != 1 || model.HamMessages != 1 {
		t.Fatalf("learner not trained: %+v", model)
	}

	// A repeated review must not train the same decision twice.
	if err := svc.Review(context.Background(), domain.ReviewRequest{DecisionIDs: []string{spamDecision.ID}, Action: domain.ReviewConfirm, IdempotencyKey: "rev-3"}); err != nil {
		t.Fatal(err)
	}
	model, _ = db.LoadLearningModel(context.Background(), account.ID)
	if model.SpamMessages != 1 {
		t.Fatalf("decision trained twice: %+v", model)
	}
	if _, ok, _ := db.DecisionFeatures(context.Background(), spamDecision.ID); ok {
		t.Fatal("trained features must be deleted")
	}
}

func TestScanUsesTrainedLearner(t *testing.T) {
	server := imaptest.New(t, rev2Caps())
	svc, db := newTestService(t, server)
	account := createTestAccount(t, svc, server, "acc-scan-learn")

	// Pre-train a balanced model directly, as confirmed reviews would.
	model := learning.NewModel()
	for i := 0; i < 12; i++ {
		model.Train(map[string]int{"lotteriegewinn": 3, "bonusjagd": 2, "dom:lotterie.example": 2}, learning.ClassSpam)
		model.Train(map[string]int{"projektbericht": 3, "wochenplan": 2, "dom:firma.example": 2}, learning.ClassHam)
	}
	if err := db.SaveLearningModel(context.Background(), account.ID, model); err != nil {
		t.Fatal(err)
	}

	// Spam-shaped message whose deterministic signals reach the review list;
	// the trained learner must add its own evidence group.
	server.AddMessage("INBOX", "unbekannt@lotterie.example", "Gewinn: sofort handeln",
		"lotteriegewinn bonusjagd jetzt anmelden",
		time.Now(), "Authentication-Results: mx.test; spf=fail")

	if _, err := svc.StartScan(context.Background(), account.ID); err != nil {
		t.Fatal(err)
	}
	run := waitForScan(t, svc, account.ID)
	if run.Status != domain.ScanCompleted {
		t.Fatalf("run status = %s (%s)", run.Status, run.Error)
	}
	decisions, err := db.ListDecisions(context.Background(), store.DecisionFilter{AccountID: account.ID})
	if err != nil || len(decisions) != 1 {
		t.Fatalf("decisions = %d, want 1 (err=%v)", len(decisions), err)
	}
	found := false
	for _, evidence := range decisions[0].Evidence {
		if evidence.Code == "statistical_spam" && evidence.Group == "statistical" {
			found = true
		}
	}
	if !found {
		t.Fatalf("statistical evidence missing: %+v", decisions[0].Evidence)
	}
}

func TestStartupRecoversInterruptedRuns(t *testing.T) {
	server := imaptest.New(t, rev2Caps())
	db, err := store.Open(filepath.Join(t.TempDir(), "recover.db"))
	if err != nil {
		t.Fatal(err)
	}
	account := domain.AccountConfig{ID: "acc-r", Name: "acc-r", Host: "imap.example", Port: 993, Username: "user", SecretRef: "imap/acc-r", InboxFolder: "INBOX", SentFolder: "Sent", SpamFolder: "AI_SPAM_FILTER", SafetyMode: domain.SafetySafe, Enabled: true, DryRun: true, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.UpsertAccount(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	run := domain.ScanRun{ID: "run-crash", AccountID: account.ID, Status: domain.ScanRunning, Folder: "INBOX", StartedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.CreateScanRun(context.Background(), run); err != nil {
		t.Fatal(err)
	}

	// A new agent process (new Service) must flag the stale run.
	client := mailbox.NewClientWithRoots(server.Roots, 5*time.Second)
	_ = NewWithMailbox(db, &memSecrets{values: map[string]string{}}, client)
	got, ok, err := db.ScanRun(context.Background(), "run-crash")
	if err != nil || !ok || got.Status != domain.ScanInterrupted {
		t.Fatalf("stale run not marked interrupted: %+v ok=%v err=%v", got, ok, err)
	}
	_ = db.Close()
}
