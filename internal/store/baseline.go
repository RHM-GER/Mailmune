package store

import (
	"context"
	"time"

	"github.com/RHM-GER/Mailmune/internal/domain"
	"github.com/RHM-GER/Mailmune/internal/learning"
)

// SaveBaseline persists an imported global learner together with its
// provenance. Any previous baseline with the same ID is replaced atomically.
func (s *SQLite) SaveBaseline(ctx context.Context, meta domain.LearningBaseline, model *learning.Model) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, "DELETE FROM learning_baseline_features WHERE baseline_id=?", meta.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO learning_baselines(id,version,source,license,corpus_rows,spam_messages,ham_messages,imported_at)
VALUES(?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET version=excluded.version,source=excluded.source,license=excluded.license,corpus_rows=excluded.corpus_rows,spam_messages=excluded.spam_messages,ham_messages=excluded.ham_messages,imported_at=excluded.imported_at`,
		meta.ID, meta.Version, meta.Source, meta.License, meta.CorpusRows, model.SpamMessages, model.HamMessages, formatTime(time.Now().UTC())); err != nil {
		return err
	}

	stmt, err := tx.PrepareContext(ctx, "INSERT INTO learning_baseline_features(baseline_id,token,spam_count,ham_count) VALUES(?,?,?,?)")
	if err != nil {
		return err
	}
	tokens := map[string][2]uint64{}
	for token, count := range model.SpamCounts {
		pair := tokens[token]
		pair[0] = count
		tokens[token] = pair
	}
	for token, count := range model.HamCounts {
		pair := tokens[token]
		pair[1] = count
		tokens[token] = pair
	}
	for token, pair := range tokens {
		if _, err := stmt.ExecContext(ctx, meta.ID, token, pair[0], pair[1]); err != nil {
			stmt.Close()
			return err
		}
	}
	if err := stmt.Close(); err != nil {
		return err
	}
	return tx.Commit()
}

// LoadBaseline returns the baseline model and its provenance, if present.
func (s *SQLite) LoadBaseline(ctx context.Context, id string) (*learning.Model, domain.LearningBaseline, bool, error) {
	var meta domain.LearningBaseline
	var imported string
	err := s.db.QueryRowContext(ctx, "SELECT id,version,source,license,corpus_rows,spam_messages,ham_messages,imported_at FROM learning_baselines WHERE id=?", id).
		Scan(&meta.ID, &meta.Version, &meta.Source, &meta.License, &meta.CorpusRows, &meta.SpamMessages, &meta.HamMessages, &imported)
	if isNoRows(err) {
		return nil, domain.LearningBaseline{}, false, nil
	}
	if err != nil {
		return nil, domain.LearningBaseline{}, false, err
	}
	meta.ImportedAt, _ = time.Parse(time.RFC3339Nano, imported)

	model := learning.NewModel()
	rows, err := s.db.QueryContext(ctx, "SELECT token,spam_count,ham_count FROM learning_baseline_features WHERE baseline_id=?", id)
	if err != nil {
		return nil, domain.LearningBaseline{}, false, err
	}
	for rows.Next() {
		var token string
		var spam, ham uint64
		if err := rows.Scan(&token, &spam, &ham); err != nil {
			rows.Close()
			return nil, domain.LearningBaseline{}, false, err
		}
		if spam > 0 {
			model.SpamCounts[token] = spam
		}
		if ham > 0 {
			model.HamCounts[token] = ham
		}
		model.SpamTokens += spam
		model.HamTokens += ham
	}
	if err := rows.Close(); err != nil {
		return nil, domain.LearningBaseline{}, false, err
	}
	model.Version = meta.Version
	model.SpamMessages = meta.SpamMessages
	model.HamMessages = meta.HamMessages
	return model, meta, true, nil
}

// ListBaselines returns the provenance of every stored baseline.
func (s *SQLite) ListBaselines(ctx context.Context) ([]domain.LearningBaseline, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,version,source,license,corpus_rows,spam_messages,ham_messages,imported_at FROM learning_baselines ORDER BY imported_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []domain.LearningBaseline
	for rows.Next() {
		var meta domain.LearningBaseline
		var imported string
		if err := rows.Scan(&meta.ID, &meta.Version, &meta.Source, &meta.License, &meta.CorpusRows, &meta.SpamMessages, &meta.HamMessages, &imported); err != nil {
			return nil, err
		}
		meta.ImportedAt, _ = time.Parse(time.RFC3339Nano, imported)
		list = append(list, meta)
	}
	return list, rows.Err()
}

// DeleteBaseline removes a baseline and its features. Confirmed per-account
// learning is never affected.
func (s *SQLite) DeleteBaseline(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM learning_baselines WHERE id=?", id)
	return err
}
