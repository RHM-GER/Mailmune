package store

import (
	"context"
	"testing"

	"github.com/RHM-GER/Mailmune/internal/domain"
	"github.com/RHM-GER/Mailmune/internal/learning"
)

func TestBaselineRoundTrip(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	model := learning.NewModel()
	for i := 0; i < 10; i++ {
		model.Train(map[string]int{"gewinn": 3, "lotterie": 2}, learning.ClassSpam)
		model.Train(map[string]int{"meeting": 2, "projekt": 2}, learning.ClassHam)
	}
	meta := domain.LearningBaseline{ID: "global", Version: learning.Version, Source: "test-corpus", License: "MIT", CorpusRows: 20}
	if err := store.SaveBaseline(ctx, meta, model); err != nil {
		t.Fatal(err)
	}

	loaded, gotMeta, ok, err := store.LoadBaseline(ctx, "global")
	if err != nil || !ok {
		t.Fatalf("baseline not found: ok=%v err=%v", ok, err)
	}
	if loaded.SpamMessages != 10 || loaded.HamMessages != 10 {
		t.Fatalf("message counts lost: %+v", loaded)
	}
	if loaded.SpamCounts["gewinn"] != 30 || loaded.HamCounts["projekt"] != 20 {
		t.Fatalf("token counts lost: %+v", loaded)
	}
	if gotMeta.Source != "test-corpus" || gotMeta.License != "MIT" || gotMeta.CorpusRows != 20 {
		t.Fatalf("provenance lost: %+v", gotMeta)
	}

	// Saving again with the same ID replaces, not duplicates.
	if err := store.SaveBaseline(ctx, meta, model); err != nil {
		t.Fatal(err)
	}
	list, err := store.ListBaselines(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("expected exactly one baseline, got %d (err=%v)", len(list), err)
	}

	if err := store.DeleteBaseline(ctx, "global"); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, _ := store.LoadBaseline(ctx, "global"); ok {
		t.Fatal("baseline survived deletion")
	}
}

func TestBaselineIsSeparateFromAccountLearning(t *testing.T) {
	store := openTestStore(t)
	insertTestAccount(t, store, "base-1")
	ctx := context.Background()

	baseline := learning.NewModel()
	baseline.Train(map[string]int{"basistoken": 5}, learning.ClassSpam)
	baseline.Train(map[string]int{"basisham": 5}, learning.ClassHam)
	if err := store.SaveBaseline(ctx, domain.LearningBaseline{ID: "global", Version: learning.Version, Source: "x", License: "MIT"}, baseline); err != nil {
		t.Fatal(err)
	}

	// Deleting the account's own learning must not touch the baseline.
	if err := store.ResetLearningModel(ctx, "base-1"); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, err := store.LoadBaseline(ctx, "global"); err != nil || !ok {
		t.Fatalf("baseline must survive account reset: ok=%v err=%v", ok, err)
	}

	// Deleting the baseline must not create account learning.
	if err := store.DeleteBaseline(ctx, "global"); err != nil {
		t.Fatal(err)
	}
	accountModel, err := store.LoadLearningModel(ctx, "base-1")
	if err != nil || accountModel.Trained() != 0 {
		t.Fatalf("account learning contaminated by baseline: %+v err=%v", accountModel, err)
	}
}
