package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/RHM-GER/Mailmune/internal/domain"
)

type SQLite struct{ db *sql.DB }

func Open(path string) (*SQLite, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &SQLite{db: db}
	if err := store.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLite) Close() error { return s.db.Close() }

func (s *SQLite) UpsertAccount(ctx context.Context, account domain.AccountConfig) error {
	profile, err := json.Marshal(account.Profile)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO accounts
(id,name,host,port,username,secret_ref,inbox_folder,sent_folder,spam_folder,safety_mode,ollama_model,ollama_validated,enabled,dry_run,profile_json,last_scan_at,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET name=excluded.name,host=excluded.host,port=excluded.port,username=excluded.username,
secret_ref=excluded.secret_ref,inbox_folder=excluded.inbox_folder,sent_folder=excluded.sent_folder,spam_folder=excluded.spam_folder,
safety_mode=excluded.safety_mode,ollama_model=excluded.ollama_model,ollama_validated=excluded.ollama_validated,
enabled=excluded.enabled,dry_run=excluded.dry_run,profile_json=excluded.profile_json,last_scan_at=excluded.last_scan_at,updated_at=excluded.updated_at`,
		account.ID, account.Name, account.Host, account.Port, account.Username, account.SecretRef,
		account.InboxFolder, account.SentFolder, account.SpamFolder, account.SafetyMode, account.OllamaModel,
		account.OllamaValidated, account.Enabled, account.DryRun, string(profile), nullableTime(account.LastScanAt),
		formatTime(account.CreatedAt), formatTime(account.UpdatedAt))
	return err
}

func (s *SQLite) ListAccounts(ctx context.Context) ([]domain.AccountConfig, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,host,port,username,secret_ref,inbox_folder,sent_folder,spam_folder,
safety_mode,ollama_model,ollama_validated,enabled,dry_run,profile_json,last_scan_at,created_at,updated_at FROM accounts ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var accounts []domain.AccountConfig
	for rows.Next() {
		var a domain.AccountConfig
		var profile string
		var last sql.NullString
		var created, updated string
		if err := rows.Scan(&a.ID, &a.Name, &a.Host, &a.Port, &a.Username, &a.SecretRef, &a.InboxFolder, &a.SentFolder, &a.SpamFolder,
			&a.SafetyMode, &a.OllamaModel, &a.OllamaValidated, &a.Enabled, &a.DryRun, &profile, &last, &created, &updated); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(profile), &a.Profile); err != nil {
			return nil, err
		}
		a.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		a.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		if last.Valid {
			parsed, _ := time.Parse(time.RFC3339Nano, last.String)
			a.LastScanAt = &parsed
		}
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}

func (s *SQLite) Account(ctx context.Context, id string) (domain.AccountConfig, error) {
	accounts, err := s.ListAccounts(ctx)
	if err != nil {
		return domain.AccountConfig{}, err
	}
	for _, account := range accounts {
		if account.ID == id {
			return account, nil
		}
	}
	return domain.AccountConfig{}, sql.ErrNoRows
}

func (s *SQLite) SaveDecision(ctx context.Context, d domain.MessageDecision) error {
	evidence, err := json.Marshal(d.Evidence)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO decisions
(id,account_id,uid_validity,uid,message_id_hash,origin_folder,current_folder,sender,subject,score,status,evidence_json,model_version,idempotency_key,received_at,created_at,reviewed_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(account_id,uid_validity,uid,origin_folder) DO NOTHING`,
		d.ID, d.AccountID, d.UIDValidity, d.UID, d.MessageIDHash, d.OriginFolder, d.CurrentFolder, d.From, d.Subject, d.Score, d.Status,
		string(evidence), d.ModelVersion, d.IdempotencyKey, formatTime(d.ReceivedAt), formatTime(d.CreatedAt), nullableTime(d.ReviewedAt))
	return err
}

// UpdateDecisionFolder records the new current folder (and destination UID) of
// a decision after a completed move, so the review list reflects reality.
func (s *SQLite) UpdateDecisionFolder(ctx context.Context, decisionID, currentFolder string, destUID uint32, at time.Time) error {
	query := "UPDATE decisions SET current_folder=?"
	args := []any{currentFolder}
	if destUID != 0 {
		query += ",uid=?"
		args = append(args, destUID)
	}
	query += " WHERE id=?"
	args = append(args, decisionID)
	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}

type DecisionFilter struct {
	Status    string
	AccountID string
	Query     string
	Since     *time.Time
	Limit     int
}

func (s *SQLite) ListDecisions(ctx context.Context, f DecisionFilter) ([]domain.MessageDecision, error) {
	query := `SELECT ` + decisionColumns + ` FROM decisions WHERE 1=1`
	args := []any{}
	if f.Status != "" {
		query += " AND status=?"
		args = append(args, f.Status)
	}
	if f.AccountID != "" {
		query += " AND account_id=?"
		args = append(args, f.AccountID)
	}
	if f.Query != "" {
		query += " AND (sender LIKE ? OR subject LIKE ?)"
		value := "%" + f.Query + "%"
		args = append(args, value, value)
	}
	if f.Since != nil {
		query += " AND received_at>=?"
		args = append(args, formatTime(*f.Since))
	}
	query += " ORDER BY received_at DESC LIMIT ?"
	if f.Limit <= 0 || f.Limit > 1000 {
		f.Limit = 250
	}
	args = append(args, f.Limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []domain.MessageDecision
	for rows.Next() {
		decision, err := scanDecision(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, decision)
	}
	return list, rows.Err()
}

const decisionColumns = `id,account_id,uid_validity,uid,message_id_hash,origin_folder,current_folder,sender,subject,score,status,evidence_json,model_version,idempotency_key,received_at,created_at,reviewed_at,trained_at`

// DecisionsByIDs returns the decisions with the given IDs, if any.
func (s *SQLite) DecisionsByIDs(ctx context.Context, ids []string) ([]domain.MessageDecision, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	query := "SELECT " + decisionColumns + " FROM decisions WHERE id IN (" + placeholders + ")"
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []domain.MessageDecision
	for rows.Next() {
		decision, err := scanDecision(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, decision)
	}
	return list, rows.Err()
}

// ReviewedDecisions returns the human-confirmed (spam) and rejected (ham)
// decisions of an account. They are the ground truth for calibration; pending
// and deferred decisions are excluded.
func (s *SQLite) ReviewedDecisions(ctx context.Context, accountID string) ([]domain.MessageDecision, error) {
	query := "SELECT " + decisionColumns + " FROM decisions WHERE account_id=? AND status IN (?,?)"
	rows, err := s.db.QueryContext(ctx, query, accountID, string(domain.StatusConfirmed), string(domain.StatusRejected))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []domain.MessageDecision
	for rows.Next() {
		decision, err := scanDecision(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, decision)
	}
	return list, rows.Err()
}

func scanDecision(rows *sql.Rows) (domain.MessageDecision, error) {
	var d domain.MessageDecision
	var evidence, received, created string
	var reviewed, trained sql.NullString
	if err := rows.Scan(&d.ID, &d.AccountID, &d.UIDValidity, &d.UID, &d.MessageIDHash, &d.OriginFolder, &d.CurrentFolder, &d.From, &d.Subject, &d.Score, &d.Status, &evidence, &d.ModelVersion, &d.IdempotencyKey, &received, &created, &reviewed, &trained); err != nil {
		return domain.MessageDecision{}, err
	}
	_ = json.Unmarshal([]byte(evidence), &d.Evidence)
	d.ReceivedAt, _ = time.Parse(time.RFC3339Nano, received)
	d.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	if reviewed.Valid {
		parsed, _ := time.Parse(time.RFC3339Nano, reviewed.String)
		d.ReviewedAt = &parsed
	}
	if trained.Valid {
		parsed, _ := time.Parse(time.RFC3339Nano, trained.String)
		d.TrainedAt = &parsed
	}
	return d, nil
}

func (s *SQLite) ApplyReview(ctx context.Context, req domain.ReviewRequest) error {
	if len(req.DecisionIDs) == 0 || req.IdempotencyKey == "" {
		return errors.New("decision IDs and idempotency key are required")
	}
	if len(req.DecisionIDs) > 500 {
		return errors.New("at most 500 decisions can be reviewed at once")
	}
	if req.Action != domain.ReviewConfirm && req.Action != domain.ReviewReject && req.Action != domain.ReviewDefer {
		return errors.New("unsupported review action")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "INSERT INTO review_operations(idempotency_key,action,created_at) VALUES(?,?,?) ON CONFLICT DO NOTHING", req.IdempotencyKey, req.Action, formatTime(time.Now()))
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return tx.Commit()
	}
	status := domain.StatusDeferred
	if req.Action == domain.ReviewConfirm {
		status = domain.StatusConfirmed
	} else if req.Action == domain.ReviewReject {
		status = domain.StatusRejected
	}
	for _, id := range req.DecisionIDs {
		if _, err := tx.ExecContext(ctx, "UPDATE decisions SET status=?,reviewed_at=? WHERE id=?", status, formatTime(time.Now()), id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLite) Summary(ctx context.Context) (domain.DashboardSummary, error) {
	var out domain.DashboardSummary
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM accounts").Scan(&out.Accounts); err != nil {
		return out, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT status,COUNT(*) FROM decisions GROUP BY status")
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return out, err
		}
		switch domain.DecisionStatus(status) {
		case domain.StatusPending:
			out.Pending = count
		case domain.StatusMoved:
			out.Moved = count
		case domain.StatusConfirmed:
			out.Confirmed = count
		case domain.StatusRejected:
			out.Rejected = count
		}
	}
	since := formatTime(time.Now().AddDate(0, 0, -7))
	_ = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM decisions WHERE created_at>=?", since).Scan(&out.ProcessedWeek)
	denominator := out.Confirmed + out.Rejected
	if denominator > 0 {
		out.FalsePositive = float64(out.Rejected) / float64(denominator)
	}
	return out, rows.Err()
}

func (s *SQLite) PurgeReadableMetadata(ctx context.Context, before time.Time) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	boundary := formatTime(before)
	if _, err := tx.ExecContext(ctx, "DELETE FROM decision_features WHERE decision_id IN (SELECT id FROM decisions WHERE created_at<?)", boundary); err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE decisions SET sender='[entfernt]',subject='[entfernt]' WHERE created_at<? AND sender<>'[entfernt]'`, boundary)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		value = time.Now().UTC()
	}
	return value.UTC().Format(time.RFC3339Nano)
}
func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return formatTime(*value)
}

func (s *SQLite) Health(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("database: %w", err)
	}
	return nil
}
