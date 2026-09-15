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

func TestSaveDecisionDedupReturnsExistingID(t *testing.T) {
	store := openTestStore(t)
	insertTestAccount(t, store, "dedup-1")
	ctx := context.Background()

	original := domain.MessageDecision{
		ID: "orig-1", AccountID: "dedup-1", UIDValidity: 5, UID: 7, MessageIDHash: "h",
		OriginFolder: "INBOX", CurrentFolder: "INBOX", From: "a@b.example", Subject: "s",
		Score: 0.7, Status: domain.StatusPending, IdempotencyKey: "k-dedup",
		ReceivedAt: time.Now(), CreatedAt: time.Now(),
	}
	storedID, created, err := store.SaveDecision(ctx, original)
	if err != nil || !created || storedID != "orig-1" {
		t.Fatalf("first insert: id=%s created=%v err=%v", storedID, created, err)
	}

	// Same message coordinates, fresh ID (as after a resync): the insert is a
	// no-op and the EXISTING id must come back so dependent rows attach.
	duplicate := original
	duplicate.ID = "dup-2"
	duplicate.IdempotencyKey = "k-dedup-2"
	storedID, created, err = store.SaveDecision(ctx, duplicate)
	if err != nil || created || storedID != "orig-1" {
		t.Fatalf("dedup: id=%s created=%v err=%v", storedID, created, err)
	}

	// Features attached to the returned ID must not violate the FK.
	if err := store.SaveDecisionFeatures(ctx, storedID, map[string]int{"token": 1}); err != nil {
		t.Fatalf("features on deduped decision failed: %v", err)
	}
}

func TestRefreshPendingDecisionUpdatesOnlyPending(t *testing.T) {
	store := openTestStore(t)
	insertTestAccount(t, store, "refresh-1")
	ctx := context.Background()

	pending := domain.MessageDecision{
		ID: "p1", AccountID: "refresh-1", UIDValidity: 1, UID: 1, MessageIDHash: "h1",
		OriginFolder: "INBOX", CurrentFolder: "INBOX", From: "a@b.example", Subject: "s",
		Score: 0.6, Status: domain.StatusPending, IdempotencyKey: "k-p1",
		ReceivedAt: time.Now(), CreatedAt: time.Now(),
	}
	if _, _, err := store.SaveDecision(ctx, pending); err != nil {
		t.Fatal(err)
	}
	confirmed := pending
	confirmed.ID = "c1"
	confirmed.UID = 2
	confirmed.IdempotencyKey = "k-c1"
	confirmed.Status = domain.StatusConfirmed
	confirmed.Score = 0.7
	if _, _, err := store.SaveDecision(ctx, confirmed); err != nil {
		t.Fatal(err)
	}

	evidence := []domain.Evidence{{Group: "model", Code: "local_model_spam", Weight: 0.9, Summary: "x"}}
	updated, err := store.RefreshPendingDecision(ctx, "p1", 0.93, evidence, "llama3", "Neuer Betreff")
	if err != nil || !updated {
		t.Fatalf("pending refresh: updated=%v err=%v", updated, err)
	}
	got, _ := store.DecisionsByIDs(ctx, []string{"p1"})
	if len(got) != 1 || got[0].Score != 0.93 || got[0].ModelVersion != "llama3" || got[0].Subject != "Neuer Betreff" {
		t.Fatalf("pending decision not refreshed: %+v", got)
	}

	// A confirmed decision is ground truth and must never change.
	updated, err = store.RefreshPendingDecision(ctx, "c1", 0.99, evidence, "llama3", "egal")
	if err != nil || updated {
		t.Fatalf("confirmed refresh must be a no-op: updated=%v err=%v", updated, err)
	}
	got, _ = store.DecisionsByIDs(ctx, []string{"c1"})
	if len(got) != 1 || got[0].Score != 0.7 {
		t.Fatalf("confirmed decision changed: %+v", got)
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
	if _, _, err := store.SaveDecision(ctx, decision); err != nil {
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
	if _, _, err := store.SaveDecision(ctx, decision); err != nil {
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
