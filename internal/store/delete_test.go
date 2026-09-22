package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/RHM-GER/Mailmune/internal/domain"
)

// TestDeleteAccountCascades proves that deleting a profile removes all of its
// derived local data (decisions, arrival counters, daily stats) while another
// account's data stays untouched. It never touches IMAP - this is purely the
// local store side of the no-delete guarantee for EMAILS.
func TestDeleteAccountCascades(t *testing.T) {
	store := openTestStore(t)
	insertTestAccount(t, store, "del-a")
	insertTestAccount(t, store, "del-b")
	ctx := context.Background()
	now := time.Now().UTC()

	seed := func(accountID, id string, uid uint32, received time.Time) {
		d := domain.MessageDecision{
			ID: id, AccountID: accountID, UIDValidity: 1, UID: uid, MessageIDHash: "h-" + id,
			OriginFolder: "INBOX", CurrentFolder: "INBOX", From: "x@y.example", Subject: "s",
			Score: 0.9, Status: domain.StatusPending, IdempotencyKey: "k-" + id,
			ReceivedAt: received, CreatedAt: received,
		}
		if _, _, err := store.SaveDecision(ctx, d); err != nil {
			t.Fatal(err)
		}
		if _, err := store.LogReceived(ctx, accountID, "h-"+id, received); err != nil {
			t.Fatal(err)
		}
	}
	seed("del-a", "a1", 1, now.Add(-time.Hour))
	seed("del-a", "a2", 2, now.Add(-2*time.Hour))
	seed("del-b", "b1", 3, now.Add(-time.Hour))
	if err := store.RecordDailyStats(ctx, "del-a", "", 5, 1, 0, 0); err != nil {
		t.Fatal(err)
	}

	if err := store.DeleteAccount(ctx, "del-a"); err != nil {
		t.Fatal(err)
	}
	// Account is gone; deleting again reports not-found.
	if _, err := store.Account(ctx, "del-a"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("account still present after delete: %v", err)
	}
	if err := store.DeleteAccount(ctx, "del-a"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("second delete = %v, want ErrNoRows", err)
	}
	// Cascades: decisions and arrival counters of del-a are removed.
	decisions, err := store.ListDecisions(ctx, DecisionFilter{AccountID: "del-a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 0 {
		t.Fatalf("decisions survived account deletion: %d", len(decisions))
	}
	summary, err := store.Summary(ctx, "del-a")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Scanned != 0 || summary.Pending != 0 {
		t.Fatalf("arrival counters survived account deletion: %+v", summary)
	}
	// daily_stats has no foreign key; it must be cleared explicitly.
	var statsRows int
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM daily_stats WHERE account_id='del-a'").Scan(&statsRows); err != nil {
		t.Fatal(err)
	}
	if statsRows != 0 {
		t.Fatalf("daily_stats survived account deletion: %d rows", statsRows)
	}
	// The other account is untouched.
	other, err := store.ListDecisions(ctx, DecisionFilter{AccountID: "del-b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 1 {
		t.Fatalf("other account's decisions changed: %d", len(other))
	}
}
