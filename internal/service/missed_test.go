package service

import (
	"context"
	"testing"
	"time"

	"github.com/RHM-GER/Mailmune/internal/domain"
	"github.com/RHM-GER/Mailmune/internal/mailbox/imaptest"
)

// TestMissedSpamCountedFromSpamFolder asserts the "Nicht erkannt" pipeline:
// a message a human (or an external filter) placed into the spam folder that
// Mailmune never flagged is counted once per received day, while Mailmune's
// own detections in the same folder are not counted, and rescans never
// double-count.
func TestMissedSpamCountedFromSpamFolder(t *testing.T) {
	server := imaptest.New(t, rev2Caps())
	svc, _ := newTestService(t, server)
	account := createTestAccount(t, svc, server, "acc-missed")
	ctx := context.Background()
	now := time.Now()

	// Spam Mailmune selbst erkennt (bleibt als Decision mit hohem Score).
	spamMessage(server, "Gewinn: Konto gesperrt, sofort handeln", now.Add(-40*time.Minute))
	// Normale Eingangsmail.
	server.AddMessage("INBOX", "freund@example.com", "Hallo", "Viele Gruesse bis morgen", now.Add(-30*time.Minute))
	// Mensch hat diese Mail manuell in den Spam-Ordner sortiert, ohne dass
	// Mailmune sie je geflaggt hat -> "Nicht erkannt".
	server.AddMessage("AI_SPAM_FILTER", "pillshop@example.com", "Billige Pillen hier klicken", "Kaufen Sie jetzt", now.Add(-20*time.Minute))

	if _, err := svc.StartScan(ctx, account.ID, false); err != nil {
		t.Fatal(err)
	}
	if run := waitForScan(t, svc, account.ID); run.Status != domain.ScanCompleted {
		t.Fatalf("run: %s (%s)", run.Status, run.Error)
	}

	today := time.Now().UTC().Format("2006-01-02")
	missedToday := func() int {
		series, err := svc.Stats(ctx, 7, "")
		if err != nil {
			t.Fatal(err)
		}
		for _, stat := range series {
			if stat.Day == today {
				return stat.Missed
			}
		}
		return 0
	}
	if got := missedToday(); got != 1 {
		t.Fatalf("Nicht erkannt = %d, want 1 (nur die manuell einsortierte Mail)", got)
	}

	// Ein kompletter Rescan darf den Sweep nicht doppelt zählen.
	if _, err := svc.StartScan(ctx, account.ID, true); err != nil {
		t.Fatal(err)
	}
	if run := waitForScan(t, svc, account.ID); run.Status != domain.ScanCompleted {
		t.Fatalf("rescan: %s (%s)", run.Status, run.Error)
	}
	if got := missedToday(); got != 1 {
		t.Fatalf("Nicht erkannt nach Rescan = %d, want 1", got)
	}
}
