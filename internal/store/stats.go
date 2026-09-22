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

// LogReceived records that a message arrived, for the dashboard's "Eingang"
// counter. Privacy: only the arrival day and the message-ID hash are stored
// (the same hash decisions already carry) - never sender, subject or text.
// Deduplicated by (account, hash), so rescans never double-count. It reports
// whether this message was logged for the first time.
func (s *SQLite) LogReceived(ctx context.Context, accountID, messageIDHash string, receivedAt time.Time) (bool, error) {
	if receivedAt.IsZero() {
		receivedAt = time.Now()
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO received_log(account_id,message_id_hash,received_day,created_at)
VALUES(?,?,?,?) ON CONFLICT(account_id,message_id_hash) DO NOTHING`,
		accountID, messageIDHash, receivedAt.UTC().Format(statsDay), formatTime(time.Now()))
	if err != nil {
		return false, err
	}
	affected, _ := result.RowsAffected()
	return affected > 0, nil
}

// receivedLogRetention bounds how long arrival counters are kept. Two years
// cover every dashboard range; older rows are pure metadata and are purged.
const receivedLogRetention = 730

// PurgeReceivedLog deletes arrival counters older than the retention window.
func (s *SQLite) PurgeReceivedLog(ctx context.Context) (int64, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -receivedLogRetention).Format(statsDay)
	result, err := s.db.ExecContext(ctx, `DELETE FROM received_log WHERE received_day < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	affected, _ := result.RowsAffected()
	return affected, nil
}

// StatsByReceivedDay aggregates the arrival counters and stored decisions by the EMAIL RECEIVED date
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
// subsets of everything that arrived). The arrival total comes from the
// privacy-preserving received_log, so it is complete in normal operation;
// decisions alone only contain candidates. Days scanned before received_log
// existed are clamped so the invariant still holds.
func (s *SQLite) StatsByReceivedDay(ctx context.Context, days int, accountID string) ([]domain.DailyStat, error) {
	if days <= 0 || days > 3660 {
		days = 30
	}
	since := time.Now().UTC().AddDate(0, 0, -days).Format(statsDay)
	// received_at is RFC3339 UTC, so the first 10 chars are the YYYY-MM-DD day.
	// accountID leer = alle Postfächer (Demo-/Gesamtansicht), sonst strikt nur
	// das aktive Profil - Daten verschiedener Profile werden nie gemischt.
	accountFilter := ""
	args := []any{since, since}
	if accountID != "" {
		accountFilter = " AND account_id = ?"
		args = []any{since, accountID, since, accountID}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT day, MAX(received), 0, MAX(spam), MAX(rejected) FROM (
SELECT received_day AS day, COUNT(*) AS received, 0 AS spam, 0 AS rejected
FROM received_log WHERE received_day >= ?`+accountFilter+` GROUP BY received_day
UNION ALL
SELECT substr(received_at,1,10) AS day, COUNT(*) AS received,
SUM(CASE WHEN score >= 0.60 AND status != 'rejected' THEN 1 ELSE 0 END) AS spam,
SUM(CASE WHEN status='rejected' THEN 1 ELSE 0 END) AS rejected
FROM decisions WHERE substr(received_at,1,10) >= ?`+accountFilter+` GROUP BY substr(received_at,1,10)
) GROUP BY day ORDER BY day ASC`, args...)
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
		// Legacy days (decisions stored before received_log existed) may lack
		// arrival rows; keep spam/false alarms a subset of the total.
		if floor := stat.Confirmed + stat.Rejected; stat.Processed < floor {
			stat.Processed = floor
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
