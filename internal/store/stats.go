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

// StatsSeries returns aggregated activity per day (across all accounts) for
// the last N days, oldest first. Days without activity are omitted; the
// frontend fills gaps.
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
