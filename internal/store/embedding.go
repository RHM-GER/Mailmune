package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/RHM-GER/Mailmune/internal/domain"
)

// SaveEmbeddingCentroids ersetzt den Zentroid-Satz eines Embedding-Modells.
func (s *SQLite) SaveEmbeddingCentroids(ctx context.Context, c domain.EmbeddingCentroids) error {
	spam, err := json.Marshal(c.SpamCenter)
	if err != nil {
		return err
	}
	ham, err := json.Marshal(c.HamCenter)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO embedding_centroids(model,dim,spam_center,ham_center,spam_n,ham_n,source,license,created_at)
VALUES(?,?,?,?,?,?,?,?,?)
ON CONFLICT(model) DO UPDATE SET dim=excluded.dim,spam_center=excluded.spam_center,ham_center=excluded.ham_center,
spam_n=excluded.spam_n,ham_n=excluded.ham_n,source=excluded.source,license=excluded.license,created_at=excluded.created_at`,
		c.Model, c.Dim, string(spam), string(ham), c.SpamN, c.HamN, c.Source, c.License, formatTime(c.CreatedAt))
	return err
}

// LoadEmbeddingCentroids liefert den Zentroid-Satz für ein Embedding-Modell.
func (s *SQLite) LoadEmbeddingCentroids(ctx context.Context, model string) (domain.EmbeddingCentroids, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT model,dim,spam_center,ham_center,spam_n,ham_n,source,license,created_at
FROM embedding_centroids WHERE model=?`, model)
	var (
		c                    domain.EmbeddingCentroids
		spamJSON, hamJSON, at string
	)
	err := row.Scan(&c.Model, &c.Dim, &spamJSON, &hamJSON, &c.SpamN, &c.HamN, &c.Source, &c.License, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.EmbeddingCentroids{}, false, nil
	}
	if err != nil {
		return domain.EmbeddingCentroids{}, false, err
	}
	if err := json.Unmarshal([]byte(spamJSON), &c.SpamCenter); err != nil {
		return domain.EmbeddingCentroids{}, false, nil
	}
	if err := json.Unmarshal([]byte(hamJSON), &c.HamCenter); err != nil {
		return domain.EmbeddingCentroids{}, false, nil
	}
	if parsed, perr := time.Parse(time.RFC3339Nano, at); perr == nil {
		c.CreatedAt = parsed
	}
	return c, true, nil
}
