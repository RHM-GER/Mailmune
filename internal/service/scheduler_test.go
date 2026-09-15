package service

import (
	"context"
	"testing"
	"time"

	"github.com/RHM-GER/Mailmune/internal/domain"
	"github.com/RHM-GER/Mailmune/internal/mailbox/imaptest"
)

// waitForScanRun polls until at least one scan run exists for the account.
func waitForScanRun(t *testing.T, svc *Service, accountID string) domain.ScanRun {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		runs, err := svc.ScanRuns(context.Background(), accountID, 5)
		if err == nil && len(runs) > 0 {
			return runs[0]
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("no scan run appeared")
	return domain.ScanRun{}
}

// waitForScanCompletion blocks until the account's newest scan run is no
// longer running. Unlike waitForScanRun, which returns as soon as the run
// record exists (before the IMAP read finishes), this guarantees the initial
// scan has fully completed, so a message added afterwards is unambiguously a
// new arrival that only the IDLE watcher can detect.
func waitForScanCompletion(t *testing.T, svc *Service, accountID string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		runs, err := svc.ScanRuns(context.Background(), accountID, 5)
		if err == nil && len(runs) > 0 && runs[0].Status != domain.ScanRunning {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("initial scan did not complete in time")
}

func TestSchedulerRunCycleTriggersReconciliation(t *testing.T) {
	server := imaptest.New(t, rev2Caps())
	svc, _ := newTestService(t, server)
	account := createTestAccount(t, svc, server, "sched-1")
	spamMessage(server, "Gewinn: sofort handeln", time.Now())

	sched := newScheduler(svc.scanner, svc.store, svc.hub, 50*time.Millisecond)
	sched.runCycle(context.Background())

	run := waitForScanRun(t, svc, account.ID)
	if run.AccountID != account.ID {
		t.Fatalf("scan run for wrong account: %+v", run)
	}
}

func TestSchedulerSkipsDisabledAccounts(t *testing.T) {
	server := imaptest.New(t, rev2Caps())
	svc, _ := newTestService(t, server)
	account := createTestAccount(t, svc, server, "sched-2")
	spamMessage(server, "Gewinn: sofort handeln", time.Now())

	// Disable the account; the scheduler must not reconcile it.
	account.Enabled = false
	if err := svc.store.UpsertAccount(context.Background(), account); err != nil {
		t.Fatal(err)
	}

	sched := newScheduler(svc.scanner, svc.store, svc.hub, 50*time.Millisecond)
	sched.runCycle(context.Background())

	runs, _ := svc.ScanRuns(context.Background(), account.ID, 5)
	if len(runs) != 0 {
		t.Fatalf("disabled account was scheduled: %d runs", len(runs))
	}
}

func TestSchedulerStartStopIsClean(t *testing.T) {
	server := imaptest.New(t, rev2Caps())
	svc, _ := newTestService(t, server)
	account := createTestAccount(t, svc, server, "sched-3")
	spamMessage(server, "Gewinn: sofort handeln", time.Now())

	sched := newScheduler(svc.scanner, svc.store, svc.hub, 30*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sched.Start(ctx)
	// A second Start must be a no-op, not a second loop.
	sched.Start(ctx)

	waitForScanRun(t, svc, account.ID)
	sched.Stop()
	sched.Stop() // idempotent
}

func TestSchedulerReconcilesNewMailOverCycles(t *testing.T) {
	server := imaptest.New(t, rev2Caps())
	svc, _ := newTestService(t, server)
	account := createTestAccount(t, svc, server, "sched-4")

	sched := newScheduler(svc.scanner, svc.store, svc.hub, 40*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sched.Start(ctx)

	// First cycle: empty mailbox, still creates a run.
	waitForScanRun(t, svc, account.ID)

	// New mail arrives; a later cycle must pick it up incrementally.
	spamMessage(server, "Konto gesperrt: sofort handeln", time.Now())
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		state, ok, _ := svc.store.FolderSyncState(context.Background(), account.ID, "INBOX")
		if ok && state.LastUID >= 1 {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	state, ok, _ := svc.store.FolderSyncState(context.Background(), account.ID, "INBOX")
	if !ok || state.LastUID < 1 {
		t.Fatalf("scheduler did not reconcile new mail: %+v ok=%v", state, ok)
	}
	sched.Stop()
}

func TestIdleWatcherTriggersScanOnNewMail(t *testing.T) {
	server := imaptest.New(t, rev2Caps())
	svc, _ := newTestService(t, server)
	account := createTestAccount(t, svc, server, "idle-1")

	// A long interval means the periodic ticker cannot fire during the test:
	// any second scan can only come from the IDLE watcher.
	sched := newScheduler(svc.scanner, svc.store, svc.hub, 30*time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sched.Start(ctx)

	// The initial cycle starts the watcher before the first scan. Wait for that
	// initial scan of the still-empty mailbox to fully complete: the watcher
	// needs fewer IMAP round-trips to reach IDLE than the scan needs to finish,
	// so once the scan is done the IDLE session is reliably established and the
	// message below can only be picked up by the watcher (the periodic ticker is
	// 30 minutes away).
	waitForScanRun(t, svc, account.ID)
	waitForScanCompletion(t, svc, account.ID)

	// New mail arrives; the server pushes EXISTS to the idling watcher, which
	// debounces and triggers an incremental scan on its own.
	spamMessage(server, "Gewinn: sofort handeln", time.Now())

	// Wait for the IDLE-triggered second run to appear and read the message.
	deadline := time.Now().Add(25 * time.Second)
	var runs []domain.ScanRun
	for time.Now().Before(deadline) {
		runs, _ = svc.ScanRuns(context.Background(), account.ID, 10)
		state, ok, _ := svc.store.FolderSyncState(context.Background(), account.ID, "INBOX")
		if len(runs) >= 2 && ok && state.LastUID >= 1 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if len(runs) < 2 {
		t.Fatalf("expected at least two scan runs (initial + IDLE-triggered), got %d", len(runs))
	}
	state, ok, _ := svc.store.FolderSyncState(context.Background(), account.ID, "INBOX")
	if !ok || state.LastUID < 1 {
		t.Fatalf("IDLE watcher did not trigger a scan: %+v ok=%v", state, ok)
	}
	sched.Stop()
}
