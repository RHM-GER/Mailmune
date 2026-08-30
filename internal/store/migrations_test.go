package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func openTestStore(t *testing.T) *SQLite {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestMigrationsApplyOnFreshDatabase(t *testing.T) {
	store := openTestStore(t)
	version, err := store.SchemaVersion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if version != LatestMigration() {
		t.Fatalf("schema version = %d, want %d", version, LatestMigration())
	}
	for _, table := range []string{"accounts", "decisions", "review_operations", "daily_stats", "folder_sync_states", "scan_runs", "schema_migrations"} {
		var name string
		if err := store.db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name); err != nil {
			t.Fatalf("table %s missing: %v", table, err)
		}
	}
}

func TestMigrationsAreIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.SchemaVersion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	second, err := store.SchemaVersion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first != second || second != LatestMigration() {
		t.Fatalf("reopen changed schema version: %d -> %d", first, second)
	}
	var rows int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != LatestMigration() {
		t.Fatalf("schema_migrations rows = %d, want %d", rows, LatestMigration())
	}
}

// Legacy databases were created with CREATE TABLE IF NOT EXISTS and have no
// schema_migrations table. Migrations must adopt them without data loss.
func TestMigrationsAdoptLegacyDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	legacy := `CREATE TABLE accounts (
 id TEXT PRIMARY KEY, name TEXT NOT NULL, host TEXT NOT NULL, port INTEGER NOT NULL,
 username TEXT NOT NULL, secret_ref TEXT NOT NULL, inbox_folder TEXT NOT NULL,
 sent_folder TEXT NOT NULL, spam_folder TEXT NOT NULL, safety_mode TEXT NOT NULL,
 ollama_model TEXT NOT NULL DEFAULT '', ollama_validated INTEGER NOT NULL DEFAULT 0,
 enabled INTEGER NOT NULL DEFAULT 1, dry_run INTEGER NOT NULL DEFAULT 1,
 profile_json TEXT NOT NULL, last_scan_at TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
INSERT INTO accounts(id,name,host,port,username,secret_ref,inbox_folder,sent_folder,spam_folder,safety_mode,profile_json,created_at,updated_at)
VALUES('legacy','Legacy','imap.example',993,'user','imap/legacy','INBOX','Sent','AI_SPAM_FILTER','safe','{}','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z');`
	if _, err := db.Exec(legacy); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	version, err := store.SchemaVersion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if version != LatestMigration() {
		t.Fatalf("schema version = %d, want %d", version, LatestMigration())
	}
	accounts, err := store.ListAccounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 1 || accounts[0].ID != "legacy" {
		t.Fatalf("legacy account lost during migration: %+v", accounts)
	}
}

func TestForeignKeysAreEnforced(t *testing.T) {
	store := openTestStore(t)
	_, err := store.db.Exec("INSERT INTO decisions(id,account_id,uid_validity,uid,message_id_hash,origin_folder,current_folder,sender,subject,score,status,evidence_json,idempotency_key,received_at,created_at) VALUES('d1','missing',1,1,'h','INBOX','INBOX','s','s',0.5,'pending','[]','k','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')")
	if err == nil {
		t.Fatal("foreign key violation not rejected")
	}
}
