package store

import (
	"context"
	"testing"
	"time"

	"github.com/RHM-GER/Mailmune/internal/domain"
)

func TestProfileModelRoundtrip(t *testing.T) {
	store := openTestStore(t)
	insertTestAccount(t, store, "pm-1")
	ctx := context.Background()

	if _, found, err := store.GetProfileModel(ctx, "pm-1"); err != nil || found {
		t.Fatalf("empty store must report not found: %v %v", found, err)
	}

	model := domain.ProfileModel{
		AccountID:  "pm-1",
		SourceHash: "hash-1",
		CompiledAt: time.Now().UTC().Truncate(time.Second),
		Model:      "qwen3:4b",
		Prompt:     "Design-Agentur-Profil",
		Indicators: domain.ProfileIndicators{
			ExpectedTopics:   []string{"design", "logo"},
			UnexpectedTopics: []domain.UnexpectedTopic{{Name: "Diät", Terms: []string{"bauchfett", "abnehmen"}}},
			Notes:            "kompiliert",
		},
		Enabled: true,
	}
	if err := store.SaveProfileModel(ctx, model); err != nil {
		t.Fatal(err)
	}
	loaded, found, err := store.GetProfileModel(ctx, "pm-1")
	if err != nil || !found {
		t.Fatalf("load failed: %v %v", found, err)
	}
	if loaded.SourceHash != "hash-1" || loaded.Prompt != "Design-Agentur-Profil" || !loaded.Enabled {
		t.Fatalf("metadata wrong: %+v", loaded)
	}
	if len(loaded.Indicators.ExpectedTopics) != 2 || len(loaded.Indicators.UnexpectedTopics) != 1 || loaded.Indicators.UnexpectedTopics[0].Name != "Diät" {
		t.Fatalf("indicators wrong: %+v", loaded.Indicators)
	}

	// Ersetzen statt Duplizieren (Primary-Key-Update).
	model.SourceHash = "hash-2"
	if err := store.SaveProfileModel(ctx, model); err != nil {
		t.Fatal(err)
	}
	if loaded, _, _ := store.GetProfileModel(ctx, "pm-1"); loaded.SourceHash != "hash-2" {
		t.Fatalf("replace failed: %+v", loaded)
	}

	if err := store.SetProfileModelEnabled(ctx, "pm-1", false); err != nil {
		t.Fatal(err)
	}
	if loaded, _, _ := store.GetProfileModel(ctx, "pm-1"); loaded.Enabled {
		t.Fatal("disable failed")
	}
}
