package store

import (
	"context"
	"time"

	"github.com/RHM-GER/Mailmune/internal/domain"
)

// PlanMoveOperation records an intended move idempotently. If an operation
// with the same idempotency key already exists, the existing row is returned
// unchanged so a repeated plan (or a crash-retry) never queues a second
// physical move.
func (s *SQLite) PlanMoveOperation(ctx context.Context, op domain.MoveOperation) (domain.MoveOperation, bool, error) {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `INSERT INTO move_operations
(id,account_id,decision_id,direction,origin_folder,origin_uid,origin_uid_validity,target_folder,state,dest_uid,dest_uid_validity,message_id_hash,attempts,last_error,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`,
		op.ID, op.AccountID, op.DecisionID, string(op.Direction), op.OriginFolder, op.OriginUID, op.OriginUIDValidity,
		op.TargetFolder, string(domain.MovePlanned), 0, 0, op.MessageIDHash, 0, "", formatTime(now), formatTime(now))
	if err != nil {
		return domain.MoveOperation{}, false, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		existing, found, err := s.MoveOperation(ctx, op.ID)
		return existing, !found, err
	}
	op.State = domain.MovePlanned
	op.CreatedAt = now
	op.UpdatedAt = now
	return op, true, nil
}

// MoveOperation loads a single operation by idempotency key.
func (s *SQLite) MoveOperation(ctx context.Context, id string) (domain.MoveOperation, bool, error) {
	row := s.db.QueryRowContext(ctx, moveOpColumns+" FROM move_operations WHERE id=?", id)
	op, err := scanMoveOperation(row)
	if err != nil {
		if isNoRows(err) {
			return domain.MoveOperation{}, false, nil
		}
		return domain.MoveOperation{}, false, err
	}
	return op, true, nil
}

// SetMoveState advances the state machine. It is a guarded update: the row is
// only changed when it is currently in one of the allowed source states, which
// makes concurrent or repeated transitions safe.
func (s *SQLite) SetMoveState(ctx context.Context, id string, from []domain.MoveState, to domain.MoveState, destUID, destValidity uint32, opErr string) (bool, error) {
	if len(opErr) > 500 {
		opErr = opErr[:500]
	}
	query := "UPDATE move_operations SET state=?,updated_at=?,attempts=attempts+1"
	args := []any{string(to), formatTime(time.Now().UTC())}
	if destUID != 0 {
		query += ",dest_uid=?,dest_uid_validity=?"
		args = append(args, destUID, destValidity)
	}
	if opErr != "" {
		query += ",last_error=?"
		args = append(args, opErr)
	} else {
		query += ",last_error=''"
	}
	query += " WHERE id=?"
	args = append(args, id)
	if len(from) > 0 {
		query += " AND state IN ("
		for i := range from {
			if i > 0 {
				query += ","
			}
			query += "?"
			args = append(args, string(from[i]))
		}
		query += ")"
	}
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	affected, _ := result.RowsAffected()
	return affected > 0, nil
}

// MoveOperationsByState lists operations of an account in the given states,
// oldest first, so a run resumes deterministically.
func (s *SQLite) MoveOperationsByState(ctx context.Context, accountID string, states []domain.MoveState, limit int) ([]domain.MoveOperation, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	query := moveOpColumns + " FROM move_operations WHERE account_id=?"
	args := []any{accountID}
	if len(states) > 0 {
		query += " AND state IN ("
		for i := range states {
			if i > 0 {
				query += ","
			}
			query += "?"
			args = append(args, string(states[i]))
		}
		query += ")"
	}
	query += " ORDER BY created_at ASC LIMIT ?"
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []domain.MoveOperation
	for rows.Next() {
		op, err := scanMoveOperation(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, op)
	}
	return list, rows.Err()
}

// RequeueStuckMoves marks operations left in an in-flight state (moving /
// restoring) as retryable on startup. They are reconciled by reading the
// mailbox before any re-attempt, never by a blind second move.
func (s *SQLite) RequeueStuckMoves(ctx context.Context) (int64, error) {
	result, err := s.db.ExecContext(ctx, "UPDATE move_operations SET state=?,updated_at=? WHERE state IN (?,?)",
		string(domain.MoveFailedRetry), formatTime(time.Now().UTC()), string(domain.MoveMoving), string(domain.MoveRestoring))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

const moveOpColumns = `SELECT id,account_id,decision_id,direction,origin_folder,origin_uid,origin_uid_validity,target_folder,state,dest_uid,dest_uid_validity,message_id_hash,attempts,last_error,created_at,updated_at`

func scanMoveOperation(row rowScanner) (domain.MoveOperation, error) {
	var op domain.MoveOperation
	var direction, state, decisionID, messageIDHash, lastError string
	var originUID, originValidity, destUID, destValidity int64
	var created, updated string
	if err := row.Scan(&op.ID, &op.AccountID, &decisionID, &direction, &op.OriginFolder, &originUID, &originValidity,
		&op.TargetFolder, &state, &destUID, &destValidity, &messageIDHash, &op.Attempts, &lastError, &created, &updated); err != nil {
		return domain.MoveOperation{}, err
	}
	op.DecisionID = decisionID
	op.Direction = domain.MoveDirection(direction)
	op.State = domain.MoveState(state)
	op.OriginUID = uint32(originUID)
	op.OriginUIDValidity = uint32(originValidity)
	op.DestUID = uint32(destUID)
	op.DestUIDValidity = uint32(destValidity)
	op.MessageIDHash = messageIDHash
	op.LastError = lastError
	op.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	op.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return op, nil
}
