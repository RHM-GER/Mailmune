package store

import (
	"context"
	"fmt"
	"time"
)

// migration is a single forward-only schema change. Migrations run in ID
// order exactly once and are recorded in schema_migrations. Existing
// migrations are never modified; schema changes always append a new one.
type migration struct {
	id    int
	name  string
	stmts []string
}

var migrations = []migration{
	{
		id:   1,
		name: "baseline",
		// Statement set of the pre-migration schema. CREATE TABLE IF NOT
		// EXISTS keeps this migration idempotent for databases that were
		// created before versioned migrations existed.
		stmts: []string{
			`CREATE TABLE IF NOT EXISTS accounts (
 id TEXT PRIMARY KEY, name TEXT NOT NULL, host TEXT NOT NULL, port INTEGER NOT NULL,
 username TEXT NOT NULL, secret_ref TEXT NOT NULL, inbox_folder TEXT NOT NULL,
 sent_folder TEXT NOT NULL, spam_folder TEXT NOT NULL, safety_mode TEXT NOT NULL,
 ollama_model TEXT NOT NULL DEFAULT '', ollama_validated INTEGER NOT NULL DEFAULT 0,
 enabled INTEGER NOT NULL DEFAULT 1, dry_run INTEGER NOT NULL DEFAULT 1,
 profile_json TEXT NOT NULL, last_scan_at TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
)`,
			`CREATE TABLE IF NOT EXISTS decisions (
 id TEXT PRIMARY KEY, account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 uid_validity INTEGER NOT NULL, uid INTEGER NOT NULL, message_id_hash TEXT NOT NULL,
 origin_folder TEXT NOT NULL, current_folder TEXT NOT NULL, sender TEXT NOT NULL,
 subject TEXT NOT NULL, score REAL NOT NULL, status TEXT NOT NULL, evidence_json TEXT NOT NULL,
 model_version TEXT NOT NULL DEFAULT '', idempotency_key TEXT NOT NULL UNIQUE,
 received_at TEXT NOT NULL, created_at TEXT NOT NULL, reviewed_at TEXT,
 UNIQUE(account_id, uid_validity, uid, origin_folder)
)`,
			`CREATE INDEX IF NOT EXISTS idx_decisions_review ON decisions(status, received_at DESC)`,
			`CREATE TABLE IF NOT EXISTS review_operations (
 idempotency_key TEXT PRIMARY KEY, action TEXT NOT NULL, created_at TEXT NOT NULL
)`,
			`CREATE TABLE IF NOT EXISTS daily_stats (
 day TEXT NOT NULL, account_id TEXT NOT NULL, processed INTEGER NOT NULL DEFAULT 0,
 moved INTEGER NOT NULL DEFAULT 0, confirmed INTEGER NOT NULL DEFAULT 0,
 	rejected INTEGER NOT NULL DEFAULT 0, PRIMARY KEY(day, account_id)
 )`,
 		},
 	},
 	{
 		id:   2,
 		name: "sync_and_scan_state",
 		stmts: []string{
 			// Per-folder UID synchronization state. A change of UIDVALIDITY
 			// invalidates every stored UID and forces a safe re-sync from
 			// UID 1 instead of a blind continuation.
 			`CREATE TABLE IF NOT EXISTS folder_sync_states (
  account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  folder TEXT NOT NULL,
  uid_validity INTEGER NOT NULL,
  last_uid INTEGER NOT NULL,
  last_sync_at TEXT NOT NULL,
  PRIMARY KEY(account_id, folder)
 )`,
 			// Background scan runs: progress, cancellation and crash
 			// recovery. Runs left in "running" state are marked
 			// "interrupted" on the next startup.
 			`CREATE TABLE IF NOT EXISTS scan_runs (
  id TEXT PRIMARY KEY,
  account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  status TEXT NOT NULL,
  folder TEXT NOT NULL,
  processed INTEGER NOT NULL DEFAULT 0,
  estimated_total INTEGER NOT NULL DEFAULT 0,
  started_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  finished_at TEXT,
  error TEXT NOT NULL DEFAULT ''
 )`,
			`CREATE INDEX IF NOT EXISTS idx_scan_runs_account ON scan_runs(account_id, started_at DESC)`,
		},
	},
	{
		id:   3,
		name: "learning",
		stmts: []string{
			// Per-account token counts of the local statistical learner.
			// Training data comes exclusively from human-confirmed reviews.
			`CREATE TABLE IF NOT EXISTS learning_features (
 account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 token TEXT NOT NULL,
 spam_count INTEGER NOT NULL DEFAULT 0,
 ham_count INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(account_id, token)
)`,
			`CREATE TABLE IF NOT EXISTS learning_meta (
 account_id TEXT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
 version INTEGER NOT NULL DEFAULT 1,
 spam_messages INTEGER NOT NULL DEFAULT 0,
 ham_messages INTEGER NOT NULL DEFAULT 0,
 updated_at TEXT NOT NULL
)`,
			// Compact feature vectors of pending decisions; deleted once the
			// decision was used for training and purged after 180 days.
			// They never contain full message texts.
			`CREATE TABLE IF NOT EXISTS decision_features (
 decision_id TEXT PRIMARY KEY REFERENCES decisions(id) ON DELETE CASCADE,
 features_json TEXT NOT NULL,
 created_at TEXT NOT NULL
)`,
			`ALTER TABLE decisions ADD COLUMN trained_at TEXT`,
		},
	},
}

func (s *SQLite) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
 id INTEGER PRIMARY KEY,
 name TEXT NOT NULL,
 applied_at TEXT NOT NULL
)`); err != nil {
		return fmt.Errorf("schema_migrations: %w", err)
	}
	applied := map[int]bool{}
	rows, err := s.db.QueryContext(ctx, "SELECT id FROM schema_migrations")
	if err != nil {
		return fmt.Errorf("read schema_migrations: %w", err)
	}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		applied[id] = true
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, m := range migrations {
		if applied[m.id] {
			continue
		}
		if err := s.applyMigration(ctx, m); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLite) applyMigration(ctx context.Context, m migration) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, stmt := range m.stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migration %d (%s): %w", m.id, m.name, err)
		}
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations(id,name,applied_at) VALUES(?,?,?)", m.id, m.name, formatTime(time.Now())); err != nil {
		return fmt.Errorf("migration %d (%s): %w", m.id, m.name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migration %d (%s): %w", m.id, m.name, err)
	}
	return nil
}

// SchemaVersion returns the highest applied migration ID, or 0 when the
// database is empty.
func (s *SQLite) SchemaVersion(ctx context.Context) (int, error) {
	var version int
	err := s.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(id),0) FROM schema_migrations").Scan(&version)
	return version, err
}

// LatestMigration reports the highest migration known to this build.
func LatestMigration() int { return migrations[len(migrations)-1].id }
