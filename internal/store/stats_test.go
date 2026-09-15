package store

import (
	"context"
	"testing"
	"time"

	"github.com/RHM-GER/Mailmune/internal/domain"
)

func TestRecordDailyStatsAccumulates(t *testing.T) {
	store := openTestStore(t)
	insertTestAccount(t, store, "stats-1")
	ctx := context.Background()
	day := time.Now().UTC().Format("2006-01-02")

	if err := store.RecordDailyStats(ctx, "stats-1", day, 10, 2, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordDailyStats(ctx, "stats-1", day, 5, 1, 3, 1); err != nil {
		t.Fatal(err)
	}
	series, err := store.StatsSeries(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 {
		t.Fatalf("series length = %d, want 1", len(series))
	}
	stat := series[0]
	if stat.Processed != 15 || stat.Moved != 3 || stat.Confirmed != 3 || stat.Rejected != 1 {
		t.Fatalf("deltas not accumulated: %+v", stat)
	}
}

func TestStatsSeriesAggregatesAccountsAndFiltersWindow(t *testing.T) {
	store := openTestStore(t)
	insertTestAccount(t, store, "stats-2")
	insertTestAccount(t, store, "stats-3")
	ctx := context.Background()
	today := time.Now().UTC().Format("2006-01-02")
	old := time.Now().UTC().AddDate(0, 0, -90).Format("2006-01-02")

	if err := store.RecordDailyStats(ctx, "stats-2", today, 4, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordDailyStats(ctx, "stats-3", today, 6, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordDailyStats(ctx, "stats-2", old, 99, 0, 0, 0); err != nil {
		t.Fatal(err)
	}

	series, err := store.StatsSeries(ctx, 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 {
		t.Fatalf("old day must fall outside the window: %+v", series)
	}
	if series[0].Processed != 10 {
		t.Fatalf("accounts not aggregated: %+v", series[0])
	}
}

func TestRecordDailyStatsNoopOnZeroDeltas(t *testing.T) {
	store := openTestStore(t)
	insertTestAccount(t, store, "stats-4")
	ctx := context.Background()
	if err := store.RecordDailyStats(ctx, "stats-4", "", 0, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	series, _ := store.StatsSeries(ctx, 7)
	if len(series) != 0 {
		t.Fatalf("zero deltas must not create rows: %+v", series)
	}
	_ = domain.DailyStat{}
}

func TestStatsByReceivedDayGroupsByReceivedDate(t *testing.T) {
	store := openTestStore(t)
	insertTestAccount(t, store, "stats-recv")
	ctx := context.Background()

	day1 := time.Now().UTC().AddDate(0, 0, -5)
	day2 := time.Now().UTC().AddDate(0, 0, -3)
	save := func(id string, uid uint32, received time.Time, score float64, status domain.DecisionStatus) {
		d := domain.MessageDecision{
			ID: id, AccountID: "stats-recv", UIDValidity: 1, UID: uid, MessageIDHash: "h-" + id,
			OriginFolder: "INBOX", CurrentFolder: "INBOX", From: "a@b.example", Subject: "s",
			Score: score, Status: status, IdempotencyKey: "k-" + id,
			ReceivedAt: received, CreatedAt: received,
		}
		if _, _, err := store.SaveDecision(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	// day1: one flagged spam (pending), one normal (below threshold), one false
	// positive (flagged then rejected).
	save("d1", 1, day1, 0.9, domain.StatusPending)
	save("d2", 2, day1, 0.2, domain.StatusPending)
	save("d3", 3, day1, 0.8, domain.StatusRejected)
	// day2: one confirmed spam.
	save("d4", 4, day2, 0.95, domain.StatusConfirmed)

	series, err := store.StatsByReceivedDay(ctx, 3660)
	if err != nil {
		t.Fatal(err)
	}
	byDay := map[string]domain.DailyStat{}
	for _, stat := range series {
		byDay[stat.Day] = stat
	}
	d1 := byDay[day1.Format("2006-01-02")]
	// Processed=total received (Eingang)=3; Confirmed(spam)=1; Rejected(false
	// alarm)=1. Spam and false alarms are both subsets of the total.
	if d1.Processed != 3 || d1.Confirmed != 1 || d1.Rejected != 1 {
		t.Fatalf("day1 aggregation wrong: %+v", d1)
	}
	d2 := byDay[day2.Format("2006-01-02")]
	if d2.Processed != 1 || d2.Confirmed != 1 || d2.Rejected != 0 {
		t.Fatalf("day2 aggregation wrong: %+v", d2)
	}
}
