package store

import (
	"context"
	"testing"
	"time"

	"github.com/RHM-GER/Mailmune/internal/domain"
)

func insertTestAccount(t *testing.T, store *SQLite, id string) {
	t.Helper()
	err := store.UpsertAccount(context.Background(), domain.AccountConfig{
		ID: id, Name: id, Host: "imap.example", Port: 993, Username: "user", SecretRef: "imap/" + id,
		InboxFolder: "INBOX", SentFolder: "Sent", SpamFolder: "AI_SPAM_FILTER", SafetyMode: domain.SafetySafe,
		Enabled: true, DryRun: true, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestFolderSyncStateRoundTrip(t *testing.T) {
	store := openTestStore(t)
	insertTestAccount(t, store, "acc-1")
	ctx := context.Background()

	if _, ok, err := store.FolderSyncState(ctx, "acc-1", "INBOX"); err != nil || ok {
		t.Fatalf("expected no state yet, ok=%v err=%v", ok, err)
	}

	state := domain.FolderSyncState{AccountID: "acc-1", Folder: "INBOX", UIDValidity: 7, LastUID: 42, LastSyncAt: time.Now().UTC().Truncate(time.Second)}
	if err := store.SaveFolderSyncState(ctx, state); err != nil {
		t.Fatal(err)
	}
	got, ok, err := store.FolderSyncState(ctx, "acc-1", "INBOX")
	if err != nil || !ok {
		t.Fatalf("state not found: ok=%v err=%v", ok, err)
	}
	if got.UIDValidity != 7 || got.LastUID != 42 {
		t.Fatalf("unexpected state: %+v", got)
	}

	// Overwrite with a new UIDVALIDITY and UID.
	state.UIDValidity = 8
	state.LastUID = 3
	if err := store.SaveFolderSyncState(ctx, state); err != nil {
		t.Fatal(err)
	}
	got, ok, err = store.FolderSyncState(ctx, "acc-1", "INBOX")
	if err != nil || !ok || got.UIDValidity != 8 || got.LastUID != 3 {
		t.Fatalf("state not updated: %+v err=%v", got, err)
	}
}

func TestScanRunLifecycle(t *testing.T) {
	store := openTestStore(t)
	insertTestAccount(t, store, "acc-2")
	ctx := context.Background()

	run := domain.ScanRun{ID: "run-1", AccountID: "acc-2", Status: domain.ScanRunning, Folder: "INBOX", StartedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := store.CreateScanRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	if active, ok, err := store.ActiveScanRun(ctx, "acc-2"); err != nil || !ok || active.ID != "run-1" {
		t.Fatalf("active run lookup failed: ok=%v err=%v run=%+v", ok, err, active)
	}
	if err := store.UpdateScanProgress(ctx, "run-1", 15, 30); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishScanRun(ctx, "run-1", domain.ScanCompleted, ""); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.ActiveScanRun(ctx, "acc-2"); err != nil || ok {
		t.Fatalf("finished run still active: ok=%v err=%v", ok, err)
	}
	got, ok, err := store.ScanRun(ctx, "run-1")
	if err != nil || !ok || got.Status != domain.ScanCompleted || got.Processed != 15 || got.EstimatedTotal != 30 {
		t.Fatalf("unexpected finished run: %+v ok=%v err=%v", got, ok, err)
	}
	if got.FinishedAt == nil {
		t.Fatal("finished run has no finish timestamp")
	}
	runs, err := store.ScanRuns(ctx, "acc-2", 0)
	if err != nil || len(runs) != 1 {
		t.Fatalf("scan run list: %v err=%v", runs, err)
	}
}

func TestMarkInterruptedRunsOnRestart(t *testing.T) {
	store := openTestStore(t)
	insertTestAccount(t, store, "acc-3")
	ctx := context.Background()
	for _, id := range []string{"run-a", "run-b"} {
		if err := store.CreateScanRun(ctx, domain.ScanRun{ID: id, AccountID: "acc-3", Status: domain.ScanRunning, Folder: "INBOX", StartedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.FinishScanRun(ctx, "run-b", domain.ScanCompleted, ""); err != nil {
		t.Fatal(err)
	}
	count, err := store.MarkInterruptedRuns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("interrupted runs = %d, want 1", count)
	}
	got, _, err := store.ScanRun(ctx, "run-a")
	if err != nil || got.Status != domain.ScanInterrupted {
		t.Fatalf("run-a status = %s, want interrupted (err=%v)", got.Status, err)
	}
	got, _, err = store.ScanRun(ctx, "run-b")
	if err != nil || got.Status != domain.ScanCompleted {
		t.Fatalf("run-b status = %s, want completed (err=%v)", got.Status, err)
	}
}
