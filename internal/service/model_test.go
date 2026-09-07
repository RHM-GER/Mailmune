package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/RHM-GER/Mailmune/internal/domain"
	"github.com/RHM-GER/Mailmune/internal/mailbox"
	"github.com/RHM-GER/Mailmune/internal/provider"
	"github.com/RHM-GER/Mailmune/internal/store"
)

// fakeOllamaServer returns a test server whose /api/generate replies with the
// given verdict JSON, and whose /api/tags lists one model.
func fakeOllamaServer(t *testing.T, verdict string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{{"name": "qwen3:4b-instruct-2507"}}})
		case "/api/generate":
			_ = json.NewEncoder(w).Encode(map[string]string{"response": verdict})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func newModelService(t *testing.T, ollamaURL string) (*Service, *store.SQLite) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "model.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := NewWithMailbox(db, &memSecrets{values: map[string]string{}}, mailbox.NewClient())
	ollama, err := provider.NewOllama(ollamaURL)
	if err != nil {
		t.Fatal(err)
	}
	svc.ollama = ollama
	return svc, db
}

func seedAccount(t *testing.T, db *store.SQLite, id string) {
	t.Helper()
	err := db.UpsertAccount(context.Background(), domain.AccountConfig{
		ID: id, Name: id, Host: "imap.example", Port: 993, Username: "user", SecretRef: "imap/" + id,
		InboxFolder: "INBOX", SentFolder: "Sent", SpamFolder: "AI_SPAM_FILTER", SafetyMode: domain.SafetySafe,
		Enabled: true, DryRun: true,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRecommendedModelsHasDefault(t *testing.T) {
	svc, _ := newModelService(t, "http://127.0.0.1:1")
	version, models := svc.RecommendedModels()
	if version == "" || len(models) == 0 {
		t.Fatalf("empty recommendations: %q %+v", version, models)
	}
	defaults := 0
	for _, model := range models {
		if model.Default {
			defaults++
		}
	}
	if defaults != 1 {
		t.Fatalf("expected exactly one default model, got %d", defaults)
	}
}

func TestSetAccountModelClearsValidation(t *testing.T) {
	svc, db := newModelService(t, "http://127.0.0.1:1")
	seedAccount(t, db, "acc-model")
	ctx := context.Background()

	// Mark validated, then change the model: validation must be cleared.
	if _, err := svc.SetAccountModel(ctx, "acc-model", "qwen3:4b-instruct-2507"); err != nil {
		t.Fatal(err)
	}
	account, _ := db.Account(ctx, "acc-model")
	if account.OllamaModel != "qwen3:4b-instruct-2507" || account.OllamaValidated {
		t.Fatalf("model selection should set model and clear validation: %+v", account)
	}
}

func TestValidateAccountModelPassesAndPersists(t *testing.T) {
	verdict, _ := json.Marshal(map[string]any{"class": "uncertain", "score": 0.4, "reasonCodes": []string{"X"}})
	server := fakeOllamaServer(t, string(verdict))
	defer server.Close()
	svc, db := newModelService(t, server.URL)
	seedAccount(t, db, "acc-valid")
	ctx := context.Background()

	report, account, err := svc.ValidateAccountModel(ctx, "acc-valid", "qwen3:4b-instruct-2507")
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed {
		t.Fatalf("capability test should pass on valid answers: %+v", report)
	}
	if !account.OllamaValidated || account.OllamaModel != "qwen3:4b-instruct-2507" {
		t.Fatalf("validation not persisted: %+v", account)
	}
	stored, _ := db.Account(ctx, "acc-valid")
	if !stored.OllamaValidated {
		t.Fatal("validated flag not stored")
	}
}

func TestValidateAccountModelFailsOnBrokenAnswers(t *testing.T) {
	server := fakeOllamaServer(t, "not json at all")
	defer server.Close()
	svc, db := newModelService(t, server.URL)
	seedAccount(t, db, "acc-invalid")
	ctx := context.Background()

	report, account, err := svc.ValidateAccountModel(ctx, "acc-invalid", "qwen3:4b-instruct-2507")
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed {
		t.Fatal("capability test must fail on broken JSON")
	}
	if account.OllamaValidated {
		t.Fatal("a failed capability test must not validate the model")
	}
}
