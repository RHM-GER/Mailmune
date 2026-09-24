package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/RHM-GER/Mailmune/internal/domain"
)

// GetProfileModel loads the compiled profile model of one account. The second
// return value is false when nothing was compiled yet.
func (s *SQLite) GetProfileModel(ctx context.Context, accountID string) (domain.ProfileModel, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT account_id,source_hash,compiled_at,model,prompt,indicators_json,enabled
FROM profile_models WHERE account_id=?`, accountID)
	var (
		m              domain.ProfileModel
		compiledAt     string
		indicatorsJSON string
		enabled        int
	)
	err := row.Scan(&m.AccountID, &m.SourceHash, &compiledAt, &m.Model, &m.Prompt, &indicatorsJSON, &enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ProfileModel{}, false, nil
	}
	if err != nil {
		return domain.ProfileModel{}, false, err
	}
	if parsed, err := time.Parse(time.RFC3339Nano, compiledAt); err == nil {
		m.CompiledAt = parsed
	}
	if err := json.Unmarshal([]byte(indicatorsJSON), &m.Indicators); err != nil {
		// Ein kaputtes Kompilat darf Scans nie blockieren; es zählt als fehlt.
		return domain.ProfileModel{}, false, nil
	}
	m.Enabled = enabled == 1
	return m, true, nil
}

// SaveProfileModel replaces the compiled profile model of one account.
func (s *SQLite) SaveProfileModel(ctx context.Context, m domain.ProfileModel) error {
	indicatorsJSON, err := json.Marshal(m.Indicators)
	if err != nil {
		return err
	}
	enabled := 0
	if m.Enabled {
		enabled = 1
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO profile_models(account_id,source_hash,compiled_at,model,prompt,indicators_json,enabled)
VALUES(?,?,?,?,?,?,?)
ON CONFLICT(account_id) DO UPDATE SET source_hash=excluded.source_hash,compiled_at=excluded.compiled_at,model=excluded.model,prompt=excluded.prompt,indicators_json=excluded.indicators_json,enabled=excluded.enabled`,
		m.AccountID, m.SourceHash, formatTime(m.CompiledAt), m.Model, m.Prompt, string(indicatorsJSON), enabled)
	return err
}

// SetProfileModelEnabled toggles whether the compiled indicators take part in
// classification. The compiled document itself stays untouched.
func (s *SQLite) SetProfileModelEnabled(ctx context.Context, accountID string, enabled bool) error {
	value := 0
	if enabled {
		value = 1
	}
	_, err := s.db.ExecContext(ctx, `UPDATE profile_models SET enabled=? WHERE account_id=?`, value, accountID)
	return err
}
