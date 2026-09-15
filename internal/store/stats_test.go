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
