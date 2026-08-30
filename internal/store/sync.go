package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/RHM-GER/Mailmune/internal/domain"
)

// FolderSyncState returns the persisted UID state of a folder. The second
// return value is false when the folder has never been synchronized.
func (s *SQLite) FolderSyncState(ctx context.Context, accountID, folder string) (domain.FolderSyncState, bool, error) {
	row := s.db.QueryRowContext(ctx, "SELECT account_id,folder,uid_validity,last_uid,last_sync_at FROM folder_sync_states WHERE account_id=? AND folder=?", accountID, folder)
	var state domain.FolderSyncState
	var validity, lastUID int64
	var synced string
	err := row.Scan(&state.AccountID, &state.Folder, &validity, &lastUID, &synced)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.FolderSyncState{}, false, nil
	}
	if err != nil {
		return domain.FolderSyncState{}, false, err
	}
	state.UIDValidity = uint32(validity)
	state.LastUID = uint32(lastUID)
	state.LastSyncAt, _ = time.Parse(time.RFC3339Nano, synced)
	return state, true, nil
}

func (s *SQLite) SaveFolderSyncState(ctx context.Context, state domain.FolderSyncState) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO folder_sync_states(account_id,folder,uid_validity,last_uid,last_sync_at)
VALUES(?,?,?,?,?)
ON CONFLICT(account_id,folder) DO UPDATE SET uid_validity=excluded.uid_validity,last_uid=excluded.last_uid,last_sync_at=excluded.last_sync_at`,
		state.AccountID, state.Folder, state.UIDValidity, state.LastUID, formatTime(state.LastSyncAt))
	return err
}

func (s *SQLite) CreateScanRun(ctx context.Context, run domain.ScanRun) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO scan_runs(id,account_id,status,folder,processed,estimated_total,started_at,updated_at,finished_at,error)
VALUES(?,?,?,?,?,?,?,?,?,?)`,
		run.ID, run.AccountID, string(run.Status), run.Folder, run.Processed, run.EstimatedTotal,
		formatTime(run.StartedAt), formatTime(run.UpdatedAt), nullableTime(run.FinishedAt), run.Error)
	return err
}

func (s *SQLite) UpdateScanProgress(ctx context.Context, id string, processed, estimatedTotal int) error {
	_, err := s.db.ExecContext(ctx, "UPDATE scan_runs SET processed=?,estimated_total=?,updated_at=? WHERE id=? AND status=?",
		processed, estimatedTotal, formatTime(time.Now().UTC()), id, string(domain.ScanRunning))
	return err
}

func (s *SQLite) FinishScanRun(ctx context.Context, id string, status domain.ScanStatus, errMsg string) error {
	if len(errMsg) > 500 {
		errMsg = errMsg[:500]
	}
	_, err := s.db.ExecContext(ctx, "UPDATE scan_runs SET status=?,updated_at=?,finished_at=?,error=? WHERE id=?",
		string(status), formatTime(time.Now().UTC()), formatTime(time.Now().UTC()), errMsg, id)
	return err
}

func (s *SQLite) ScanRun(ctx context.Context, id string) (domain.ScanRun, bool, error) {
	run, err := s.scanRow(s.db.QueryRowContext(ctx, "SELECT id,account_id,status,folder,processed,estimated_total,started_at,updated_at,finished_at,error FROM scan_runs WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ScanRun{}, false, nil
	}
	return run, err == nil, err
}

// ActiveScanRun returns the running scan of an account, if any.
func (s *SQLite) ActiveScanRun(ctx context.Context, accountID string) (domain.ScanRun, bool, error) {
	run, err := s.scanRow(s.db.QueryRowContext(ctx, "SELECT id,account_id,status,folder,processed,estimated_total,started_at,updated_at,finished_at,error FROM scan_runs WHERE account_id=? AND status=? ORDER BY started_at DESC LIMIT 1", accountID, string(domain.ScanRunning)))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ScanRun{}, false, nil
	}
	return run, err == nil, err
}

func (s *SQLite) ScanRuns(ctx context.Context, accountID string, limit int) ([]domain.ScanRun, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, "SELECT id,account_id,status,folder,processed,estimated_total,started_at,updated_at,finished_at,error FROM scan_runs WHERE account_id=? ORDER BY started_at DESC LIMIT ?", accountID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var runs []domain.ScanRun
	for rows.Next() {
		run, err := s.scanRow(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

// MarkInterruptedRuns flags scan runs that were still running when the agent
// stopped, so a restart never resumes half-finished work blindly. The next
// scan resumes from the persisted UID state instead.
func (s *SQLite) MarkInterruptedRuns(ctx context.Context) (int64, error) {
	result, err := s.db.ExecContext(ctx, "UPDATE scan_runs SET status=?,updated_at=?,finished_at=? WHERE status=?",
		string(domain.ScanInterrupted), formatTime(time.Now().UTC()), formatTime(time.Now().UTC()), string(domain.ScanRunning))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

type rowScanner interface{ Scan(dest ...any) error }

func (s *SQLite) scanRow(row rowScanner) (domain.ScanRun, error) {
	var run domain.ScanRun
	var status string
	var started, updated string
	var finished sql.NullString
	if err := row.Scan(&run.ID, &run.AccountID, &status, &run.Folder, &run.Processed, &run.EstimatedTotal, &started, &updated, &finished, &run.Error); err != nil {
		return domain.ScanRun{}, err
	}
	run.Status = domain.ScanStatus(status)
	run.StartedAt, _ = time.Parse(time.RFC3339Nano, started)
	run.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	if finished.Valid {
		parsed, _ := time.Parse(time.RFC3339Nano, finished.String)
		run.FinishedAt = &parsed
	}
	return run, nil
}
