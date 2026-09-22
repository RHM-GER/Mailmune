package service

import (
	"context"
	"testing"
	"time"

	"github.com/RHM-GER/Mailmune/internal/domain"
	"github.com/RHM-GER/Mailmune/internal/mailbox/imaptest"
	"github.com/RHM-GER/Mailmune/internal/store"
)

// TestProductionModeCountsArrivalsWithoutStoringHams asserts the dashboard's
// "Eingang" series is complete even in production mode, where below-threshold
// mail is not persisted as a decision: the privacy-preserving arrival log
// (day + message-ID hash only) still counts every message exactly once.
func TestProductionModeCountsArrivalsWithoutStoringHams(t *testing.T) {
	debugScanAllMessages = false
	t.Cleanup(func() { debugScanAllMessages = true })

	server := imaptest.New(t, rev2Caps())
	svc, db := newTestService(t, server)
	account := createTestAccount(t, svc, server, "acc-arrivals")
	ctx := context.Background()
	now := time.Now()

	spamMessage(server, "Gewinn: Konto gesperrt, sofort handeln", now.Add(-30*time.Minute))
	server.AddMessage("INBOX", "freund@example.com", "Hallo", "Viele Gruesse bis morgen", now.Add(-10*time.Minute))

	if _, err := svc.StartScan(ctx, account.ID, false); err != nil {
		t.Fatal(err)
	}
	run := waitForScan(t, svc, account.ID)
	if run.Status != domain.ScanCompleted {
		t.Fatalf("run: %s (%s)", run.Status, run.Error)
	}

	// Production mode: only the candidate is stored as a decision.
	decisions, err := db.ListDecisions(ctx, store.DecisionFilter{AccountID: account.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 {
		t.Fatalf("decisions = %d, want 1 (ham must not be persisted)", len(decisions))
	}

	// The arrival series still counts both messages on their received day.
	series, err := svc.Stats(ctx, 7, "")
	if err != nil {
		t.Fatal(err)
	}
	today := time.Now().UTC().Format("2006-01-02")
	var found *domain.DailyStat
	for i, stat := range series {
		if stat.Day == today {
			found = &series[i]
		}
	}
	if found == nil {
		t.Fatalf("today missing from arrival series: %+v", series)
	}
	if found.Processed != 2 {
		t.Fatalf("Eingang = %d, want 2 (ham + candidate)", found.Processed)
	}
	if found.Confirmed != 1 || found.Rejected != 0 {
		t.Fatalf("spam/false-alarm counts wrong: %+v", *found)
	}

	// A rescan must not double-count arrivals.
	if _, err := svc.StartScan(ctx, account.ID, true); err != nil {
		t.Fatal(err)
	}
	if rescan := waitForScan(t, svc, account.ID); rescan.Status != domain.ScanCompleted {
		t.Fatalf("rescan: %s (%s)", rescan.Status, rescan.Error)
	}
	series2, err := svc.Stats(ctx, 7, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, stat := range series2 {
		if stat.Day == today && stat.Processed != 2 {
			t.Fatalf("rescan double-counted arrivals: %+v", stat)
		}
	}
}
