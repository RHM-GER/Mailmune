package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/RHM-GER/Mailmune/internal/learning"
)

// SaveDecisionFeatures stores the compact feature vector of a decision so a
// later confirmed review can train the local model. Feature vectors contain
// normalized tokens only, never raw message text.
func (s *SQLite) SaveDecisionFeatures(ctx context.Context, decisionID string, features map[string]int) error {
	if len(features) == 0 {
		return nil
	}
	payload, err := json.Marshal(features)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO decision_features(decision_id,features_json,created_at) VALUES(?,?,?)
ON CONFLICT(decision_id) DO UPDATE SET features_json=excluded.features_json`,
		decisionID, string(payload), formatTime(time.Now().UTC()))
	return err
}

// DecisionFeatures returns the stored feature vector of a decision, if any.
func (s *SQLite) DecisionFeatures(ctx context.Context, decisionID string) (map[string]int, bool, error) {
	var payload string
	err := s.db.QueryRowContext(ctx, "SELECT features_json FROM decision_features WHERE decision_id=?", decisionID).Scan(&payload)
	if err != nil {
		if isNoRows(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	features := map[string]int{}
	if err := json.Unmarshal([]byte(payload), &features); err != nil {
		return nil, false, err
	}
	return features, true, nil
}

// MarkDecisionTrained records that a decision was used for training and
// deletes its feature vector; it is never needed twice.
func (s *SQLite) MarkDecisionTrained(ctx context.Context, decisionID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "UPDATE decisions SET trained_at=? WHERE id=?", formatTime(time.Now().UTC()), decisionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM decision_features WHERE decision_id=?", decisionID); err != nil {
		return err
	}
	return tx.Commit()
}

// LoadLearningModel rebuilds the learner for an account from persisted
// counts. An account without training data returns an empty model.
func (s *SQLite) LoadLearningModel(ctx context.Context, accountID string) (*learning.Model, error) {
	model := learning.NewModel()
	rows, err := s.db.QueryContext(ctx, "SELECT token,spam_count,ham_count FROM learning_features WHERE account_id=?", accountID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var token string
		var spam, ham uint64
		if err := rows.Scan(&token, &spam, &ham); err != nil {
			rows.Close()
			return nil, err
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
		return nil, err
	}
	var version int
	var spamMessages, hamMessages uint64
	err = s.db.QueryRowContext(ctx, "SELECT version,spam_messages,ham_messages FROM learning_meta WHERE account_id=?", accountID).
		Scan(&version, &spamMessages, &hamMessages)
	if err == nil {
		model.Version = version
		model.SpamMessages = spamMessages
		model.HamMessages = hamMessages
	} else if !isNoRows(err) {
		return nil, err
	}
	return model, nil
}

// SaveLearningModel replaces the persisted learner state of an account.
func (s *SQLite) SaveLearningModel(ctx context.Context, accountID string, model *learning.Model) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM learning_features WHERE account_id=?", accountID); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, "INSERT INTO learning_features(account_id,token,spam_count,ham_count) VALUES(?,?,?,?)")
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
		if _, err := stmt.ExecContext(ctx, accountID, token, pair[0], pair[1]); err != nil {
			stmt.Close()
			return err
		}
	}
	if err := stmt.Close(); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO learning_meta(account_id,version,spam_messages,ham_messages,updated_at) VALUES(?,?,?,?,?)
ON CONFLICT(account_id) DO UPDATE SET version=excluded.version,spam_messages=excluded.spam_messages,ham_messages=excluded.ham_messages,updated_at=excluded.updated_at`,
		accountID, model.Version, model.SpamMessages, model.HamMessages, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	return tx.Commit()
}

// ResetLearningModel removes all learned state of an account.
func (s *SQLite) ResetLearningModel(ctx context.Context, accountID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM learning_features WHERE account_id=?", accountID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM learning_meta WHERE account_id=?", accountID); err != nil {
		return err
	}
	return tx.Commit()
}

func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }
