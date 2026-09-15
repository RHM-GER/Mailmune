package store

import (
	"context"
	"time"

	"github.com/RHM-GER/Mailmune/internal/domain"
)

// statsDay is the day key format used in daily_stats.
const statsDay = "2006-01-02"

// RecordDailyStats adds deltas to the per-day, per-account counters. Missing
// rows are created; existing rows are incremented atomically.
func (s *SQLite) RecordDailyStats(ctx context.Context, accountID, day string, processed, moved, confirmed, rejected int) error {
	if processed == 0 && moved == 0 && confirmed == 0 && rejected == 0 {
		return nil
	}
	if day == "" {
		day = time.Now().UTC().Format(statsDay)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO daily_stats(day,account_id,processed,moved,confirmed,rejected)
VALUES(?,?,?,?,?,?)
ON CONFLICT(day,account_id) DO UPDATE SET processed=processed+excluded.processed,moved=moved+excluded.moved,confirmed=confirmed+excluded.confirmed,rejected=rejected+excluded.rejected`,
		day, accountID, processed, moved, confirmed, rejected)
	return err
}

// StatsByReceivedDay aggregates stored decisions by the EMAIL RECEIVED date
// (not the scan/review activity date), so the dashboard shows when spam and
// normal mail actually arrived instead of when Mailmune happened to process
// them. Per received day it returns:
//   - Processed = TOTAL mail received that day (the "Eingang" whole),
//   - Confirmed = spam: flagged (score >= 0.60) and NOT rejected, so a mail
//     later marked as a false alarm leaves this count,
//   - Rejected  = false alarms (a reviewer marked a flagged mail legit); these
//     are a subset of the originally flagged mail,
//   - Moved is unused (always 0).
// Invariant: Confirmed + Rejected <= Processed (spam and false alarms are both
// subsets of everything that arrived). Because it reads the decisions table,
// the total only includes below-threshold mail when those are stored (the
// inspect-all test mode); in normal operation only candidates are persisted.
func (s *SQLite) StatsByReceivedDay(ctx context.Context, days int) ([]domain.DailyStat, error) {
	if days <= 0 || days > 3660 {
		days = 30
	}
	since := time.Now().UTC().AddDate(0, 0, -days).Format(statsDay)
	// received_at is RFC3339 UTC, so the first 10 chars are the YYYY-MM-DD day.
	rows, err := s.db.QueryContext(ctx, `SELECT substr(received_at,1,10) AS day,
COUNT(*),
0,
SUM(CASE WHEN score >= 0.60 AND status != 'rejected' THEN 1 ELSE 0 END),
SUM(CASE WHEN status='rejected' THEN 1 ELSE 0 END)
FROM decisions WHERE substr(received_at,1,10) >= ? GROUP BY substr(received_at,1,10) ORDER BY day ASC`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var series []domain.DailyStat
	for rows.Next() {
		var stat domain.DailyStat
		if err := rows.Scan(&stat.Day, &stat.Processed, &stat.Moved, &stat.Confirmed, &stat.Rejected); err != nil {
			return nil, err
		}
		series = append(series, stat)
	}
	return series, rows.Err()
}

// StatsSeries returns aggregated ACTIVITY per day (across all accounts) for the
// last N days from the daily_stats counters. NOTE: this is keyed by the day the
// work happened, not the mail's received date, so the dashboard no longer uses
// it (see StatsByReceivedDay); it is kept for the activity counters.
func (s *SQLite) StatsSeries(ctx context.Context, days int) ([]domain.DailyStat, error) {
	if days <= 0 || days > 3660 {
		days = 30
	}
	since := time.Now().UTC().AddDate(0, 0, -days).Format(statsDay)
	rows, err := s.db.QueryContext(ctx, `SELECT day,SUM(processed),SUM(moved),SUM(confirmed),SUM(rejected)
FROM daily_stats WHERE day>=? GROUP BY day ORDER BY day ASC`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var series []domain.DailyStat
	for rows.Next() {
		var stat domain.DailyStat
		if err := rows.Scan(&stat.Day, &stat.Processed, &stat.Moved, &stat.Confirmed, &stat.Rejected); err != nil {
			return nil, err
		}
		series = append(series, stat)
	}
	return series, rows.Err()
}
