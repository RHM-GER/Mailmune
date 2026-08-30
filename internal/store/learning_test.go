package store

import (
	"context"
	"testing"
	"time"

	"github.com/RHM-GER/Mailmune/internal/domain"
	"github.com/RHM-GER/Mailmune/internal/learning"
)

func TestLearningModelRoundTrip(t *testing.T) {
	store := openTestStore(t)
	insertTestAccount(t, store, "learn-1")
	ctx := context.Background()

	model := learning.NewModel()
	model.Train(map[string]int{"gewinn": 3, "lotterie": 2, "dom:spam.example": 2}, learning.ClassSpam)
	model.Train(map[string]int{"meeting": 2, "projekt": 2, "dom:firma.example": 2}, learning.ClassHam)
	if err := store.SaveLearningModel(ctx, "learn-1", model); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.LoadLearningModel(ctx, "learn-1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SpamMessages != 1 || loaded.HamMessages != 1 {
		t.Fatalf("message counts lost: %+v", loaded)
	}
	if loaded.SpamCounts["gewinn"] != 3 || loaded.HamCounts["projekt"] != 2 {
		t.Fatalf("token counts lost: %+v", loaded)
	}
	spamScore, ok := loaded.Score(map[string]int{"gewinn": 1, "dom:spam.example": 1})
	if !ok || spamScore < 0.5 {
		t.Fatalf("reloaded model does not score spam: %.2f ok=%v", spamScore, ok)
	}

	if err := store.ResetLearningModel(ctx, "learn-1"); err != nil {
		t.Fatal(err)
	}
	empty, err := store.LoadLearningModel(ctx, "learn-1")
	if err != nil || empty.Trained() != 0 {
		t.Fatalf("reset failed: %+v err=%v", empty, err)
	}
}

func TestDecisionFeaturesLifecycle(t *testing.T) {
	store := openTestStore(t)
	insertTestAccount(t, store, "learn-2")
	ctx := context.Background()

	decision := domain.MessageDecision{
		ID: "dec-1", AccountID: "learn-2", UIDValidity: 1, UID: 1, MessageIDHash: "h",
		OriginFolder: "INBOX", CurrentFolder: "INBOX", From: "a@b.example", Subject: "s",
		Score: 0.7, Status: domain.StatusPending, IdempotencyKey: "k1",
		ReceivedAt: time.Now(), CreatedAt: time.Now(),
	}
	if err := store.SaveDecision(ctx, decision); err != nil {
		t.Fatal(err)
	}
	features := map[string]int{"gewinn": 2, "dom:b.example": 1}
	if err := store.SaveDecisionFeatures(ctx, "dec-1", features); err != nil {
		t.Fatal(err)
	}
	loaded, ok, err := store.DecisionFeatures(ctx, "dec-1")
	if err != nil || !ok || loaded["gewinn"] != 2 {
		t.Fatalf("features round trip failed: %+v ok=%v err=%v", loaded, ok, err)
	}

	if err := store.MarkDecisionTrained(ctx, "dec-1"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := store.DecisionFeatures(ctx, "dec-1"); ok {
		t.Fatal("features must be deleted after training")
	}
	matches, err := store.DecisionsByIDs(ctx, []string{"dec-1"})
	if err != nil || len(matches) != 1 || matches[0].TrainedAt == nil {
		t.Fatalf("trained_at not recorded: %+v err=%v", matches, err)
	}
}

func TestPurgeRemovesOldDecisionFeatures(t *testing.T) {
	store := openTestStore(t)
	insertTestAccount(t, store, "learn-3")
	ctx := context.Background()

	decision := domain.MessageDecision{
		ID: "dec-old", AccountID: "learn-3", UIDValidity: 1, UID: 1, MessageIDHash: "h",
		OriginFolder: "INBOX", CurrentFolder: "INBOX", From: "alt@b.example", Subject: "s",
		Score: 0.7, Status: domain.StatusPending, IdempotencyKey: "k2",
		ReceivedAt: time.Now().AddDate(0, -8, 0), CreatedAt: time.Now().AddDate(0, -8, 0),
	}
	if err := store.SaveDecision(ctx, decision); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDecisionFeatures(ctx, "dec-old", map[string]int{"alt": 1}); err != nil {
		t.Fatal(err)
	}

	if _, err := store.PurgeReadableMetadata(ctx, time.Now().AddDate(0, 0, -180)); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := store.DecisionFeatures(ctx, "dec-old"); ok {
		t.Fatal("old decision features must be purged")
	}
	matches, _ := store.DecisionsByIDs(ctx, []string{"dec-old"})
	if len(matches) != 1 {
		t.Fatal("decisions themselves must never be deleted")
	}
	if matches[0].From != "[entfernt]" || matches[0].Subject != "[entfernt]" {
		t.Fatalf("metadata not redacted: %+v", matches[0])
	}
}
