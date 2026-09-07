package store

import (
	"context"
	"testing"
	"time"

	"github.com/RHM-GER/Mailmune/internal/domain"
)

func plannedOp(id, accountID string, uid uint32) domain.MoveOperation {
	return domain.MoveOperation{
		ID: id, AccountID: accountID, Direction: domain.MoveToSpam,
		OriginFolder: "INBOX", OriginUID: uid, OriginUIDValidity: 7,
		TargetFolder: "AI_SPAM_FILTER", MessageIDHash: "h",
	}
}

func TestPlanMoveOperationIsIdempotent(t *testing.T) {
	store := openTestStore(t)
	insertTestAccount(t, store, "mv-1")
	ctx := context.Background()

	first, created, err := store.PlanMoveOperation(ctx, plannedOp("move:1", "mv-1", 10))
	if err != nil || !created {
		t.Fatalf("first plan: created=%v err=%v", created, err)
	}
	if first.State != domain.MovePlanned {
		t.Fatalf("state = %s, want planned", first.State)
	}
	// A repeated plan with the same key must not create a second move.
	second, created, err := store.PlanMoveOperation(ctx, plannedOp("move:1", "mv-1", 10))
	if err != nil || created {
		t.Fatalf("second plan: created=%v err=%v", created, err)
	}
	if second.ID != first.ID {
		t.Fatal("repeated plan changed identity")
	}
	ops, _ := store.MoveOperationsByState(ctx, "mv-1", nil, 0)
	if len(ops) != 1 {
		t.Fatalf("expected one operation, got %d", len(ops))
	}
}

func TestSetMoveStateIsGuarded(t *testing.T) {
	store := openTestStore(t)
	insertTestAccount(t, store, "mv-2")
	ctx := context.Background()
	if _, _, err := store.PlanMoveOperation(ctx, plannedOp("move:2", "mv-2", 11)); err != nil {
		t.Fatal(err)
	}

	// planned -> moving is allowed.
	ok, err := store.SetMoveState(ctx, "move:2", []domain.MoveState{domain.MovePlanned}, domain.MoveMoving, 0, 0, "")
	if err != nil || !ok {
		t.Fatalf("planned->moving: ok=%v err=%v", ok, err)
	}
	// A second planned -> moving transition must be refused (already moving).
	ok, err = store.SetMoveState(ctx, "move:2", []domain.MoveState{domain.MovePlanned}, domain.MoveMoving, 0, 0, "")
	if err != nil || ok {
		t.Fatalf("guarded transition should be refused: ok=%v err=%v", ok, err)
	}
	// moving -> moved records the destination UID.
	ok, err = store.SetMoveState(ctx, "move:2", []domain.MoveState{domain.MoveMoving}, domain.MoveMoved, 99, 7, "")
	if err != nil || !ok {
		t.Fatalf("moving->moved: ok=%v err=%v", ok, err)
	}
	op, _, _ := store.MoveOperation(ctx, "move:2")
	if op.State != domain.MoveMoved || op.DestUID != 99 {
		t.Fatalf("operation not advanced: %+v", op)
	}
	if op.Attempts < 2 {
		t.Fatalf("attempts not counted: %d", op.Attempts)
	}
}

func TestRequeueStuckMovesOnStartup(t *testing.T) {
	store := openTestStore(t)
	insertTestAccount(t, store, "mv-3")
	ctx := context.Background()
	if _, _, err := store.PlanMoveOperation(ctx, plannedOp("move:3", "mv-3", 12)); err != nil {
		t.Fatal(err)
	}
	// Simulate a crash mid-move.
	if _, err := store.SetMoveState(ctx, "move:3", []domain.MoveState{domain.MovePlanned}, domain.MoveMoving, 0, 0, ""); err != nil {
		t.Fatal(err)
	}
	count, err := store.RequeueStuckMoves(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("requeued = %d, want 1", count)
	}
	op, _, _ := store.MoveOperation(ctx, "move:3")
	if op.State != domain.MoveFailedRetry {
		t.Fatalf("stuck move not requeued: %s", op.State)
	}
}

func TestMoveOperationsFilterByState(t *testing.T) {
	store := openTestStore(t)
	insertTestAccount(t, store, "mv-4")
	ctx := context.Background()
	for i, uid := range []uint32{20, 21, 22} {
		id := "move:4:" + string(rune('a'+i))
		if _, _, err := store.PlanMoveOperation(ctx, plannedOp(id, "mv-4", uid)); err != nil {
			t.Fatal(err)
		}
	}
	// Advance one to moving.
	if _, err := store.SetMoveState(ctx, "move:4:a", []domain.MoveState{domain.MovePlanned}, domain.MoveMoving, 0, 0, ""); err != nil {
		t.Fatal(err)
	}
	planned, _ := store.MoveOperationsByState(ctx, "mv-4", []domain.MoveState{domain.MovePlanned}, 0)
	if len(planned) != 2 {
		t.Fatalf("planned ops = %d, want 2", len(planned))
	}
	moving, _ := store.MoveOperationsByState(ctx, "mv-4", []domain.MoveState{domain.MoveMoving}, 0)
	if len(moving) != 1 {
		t.Fatalf("moving ops = %d, want 1", len(moving))
	}
	_ = time.Now
}
