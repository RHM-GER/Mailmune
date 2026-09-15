package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RHM-GER/Mailmune/internal/domain"
	"github.com/RHM-GER/Mailmune/internal/learning"
	"github.com/RHM-GER/Mailmune/internal/mailbox"
	"github.com/RHM-GER/Mailmune/internal/mailbox/imaptest"
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

func TestScanPassesLearnedContextToModel(t *testing.T) {
	server := imaptest.New(t, rev2Caps())
	svc, db := newTestService(t, server)
	account := createTestAccount(t, svc, server, "acc-rag")
	ctx := context.Background()

	// Train a per-profile model with clear, discriminative signals so the
	// scanner builds a learned context (>= minLearningSamples confirmed).
	model := learning.NewModel()
	for i := 0; i < 12; i++ {
		model.Train(map[string]int{"lotteriegewinn": 3, "bonusjagd": 2, "dom:lotterie.example": 2}, learning.ClassSpam)
		model.Train(map[string]int{"projektbericht": 3, "wochenplan": 2, "dom:firma.example": 2}, learning.ClassHam)
	}
	if err := db.SaveLearningModel(ctx, account.ID, model); err != nil {
		t.Fatal(err)
	}

	// Enable a validated local model on the account.
	account.OllamaModel = "test-model"
	account.OllamaValidated = true
	if err := db.UpsertAccount(ctx, account); err != nil {
		t.Fatal(err)
	}

	// Fake Ollama that captures the prompt payload.
	var captured string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured = string(body)
		verdict, _ := json.Marshal(map[string]any{"class": "spam", "score": 0.9, "reasonCodes": []string{"TEST"}})
		_ = json.NewEncoder(w).Encode(map[string]string{"response": string(verdict)})
	}))
	t.Cleanup(fake.Close)
	ollama, err := provider.NewOllama(fake.URL)
	if err != nil {
		t.Fatal(err)
	}
	svc.scanner.ollama = ollama

	// A mildly suspicious message the rules place in the ambiguous band
	// [0.25, 0.98) so consultModel runs, but neutral to the trained model so
	// the statistical stage does not push it back out of the band.
	server.AddMessage("INBOX", "jemand@unbekannt.example", "Gratis Angebot jetzt",
		"Dies ist ein neutrales Schreiben ohne besondere Merkmale", time.Now())

	if _, err := svc.StartScan(ctx, account.ID, false); err != nil {
		t.Fatal(err)
	}
	run := waitForScan(t, svc, account.ID)
	if run.Status != domain.ScanCompleted {
		t.Fatalf("run status = %s (%s)", run.Status, run.Error)
	}
	if captured == "" {
		t.Fatal("local model was never consulted")
	}
	if !strings.Contains(captured, "learnedSignals") {
		t.Fatalf("learned context missing from model prompt: %s", captured)
	}
	// The per-profile signals (spam and ham) must reach the model.
	if !strings.Contains(captured, "lotteriegewinn") || !strings.Contains(captured, "projektbericht") {
		t.Fatalf("per-profile signals missing from model prompt: %s", captured)
	}
	// Privacy: full sender addresses from training never leak into the prompt.
	if strings.Contains(captured, "snd:") {
		t.Fatalf("raw sender marker leaked into model prompt: %s", captured)
	}
}

func TestFullScanConsultsModelOnLowScoreMessage(t *testing.T) {
	server := imaptest.New(t, rev2Caps())
	svc, db := newTestService(t, server)
	account := createTestAccount(t, svc, server, "acc-ai-full")
	ctx := context.Background()

	account.OllamaModel = "test-model"
	account.OllamaValidated = true
	if err := db.UpsertAccount(ctx, account); err != nil {
		t.Fatal(err)
	}

	var captured string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured = string(body)
		verdict, _ := json.Marshal(map[string]any{"class": "ham", "score": 0.1, "reasonCodes": []string{"TEST"}})
		_ = json.NewEncoder(w).Encode(map[string]string{"response": string(verdict)})
	}))
	t.Cleanup(fake.Close)
	ollama, err := provider.NewOllama(fake.URL)
	if err != nil {
		t.Fatal(err)
	}
	svc.scanner.ollama = ollama

	// A neutral message scores below the 0.25 incremental band, so only a full
	// scan (resync) sends it to the model.
	server.AddMessage("INBOX", "kollege@firma.example", "Meeting morgen", "Kurze Rueckfrage zum Termin", time.Now())

	if _, err := svc.StartScan(ctx, account.ID, true); err != nil { // resync = full scan
		t.Fatal(err)
	}
	run := waitForScan(t, svc, account.ID)
	if run.Status != domain.ScanCompleted {
		t.Fatalf("run: %s (%s)", run.Status, run.Error)
	}
	if captured == "" {
		t.Fatal("a full scan must consult the model even for a low-score message")
	}
}

func TestLowConfidenceSpamVerdictNeverLowersScore(t *testing.T) {
	server := imaptest.New(t, rev2Caps())
	svc, db := newTestService(t, server)
	account := createTestAccount(t, svc, server, "acc-ai-nodrop")
	ctx := context.Background()

	account.OllamaModel = "test-model"
	account.OllamaValidated = true
	if err := db.UpsertAccount(ctx, account); err != nil {
		t.Fatal(err)
	}

	// The model answers "spam" but with a tiny, badly calibrated confidence.
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		verdict, _ := json.Marshal(map[string]any{"class": "spam", "score": 0.05, "reasonCodes": []string{"TEST"}})
		_ = json.NewEncoder(w).Encode(map[string]string{"response": string(verdict)})
	}))
	t.Cleanup(fake.Close)
	ollama, err := provider.NewOllama(fake.URL)
	if err != nil {
		t.Fatal(err)
	}
	svc.scanner.ollama = ollama

	// Strong rule score (~0.87): digit-run domain + reward bait + "!!!".
	server.AddMessage("INBOX", "noreply@survey98765.example", "Sie gehören zu den 100 Glücklichen!!!", "Jetzt bestaetigen", time.Now())

	if _, err := svc.StartScan(ctx, account.ID, true); err != nil {
		t.Fatal(err)
	}
	run := waitForScan(t, svc, account.ID)
	if run.Status != domain.ScanCompleted {
		t.Fatalf("run: %s (%s)", run.Status, run.Error)
	}
	decisions, _ := db.ListDecisions(ctx, store.DecisionFilter{AccountID: account.ID})
	if len(decisions) != 1 {
		t.Fatalf("decisions = %d, want 1", len(decisions))
	}
	if decisions[0].Score < 0.8 {
		t.Fatalf("a low-confidence spam verdict lowered the score: %.3f", decisions[0].Score)
	}
}

func TestResetLearningClearsOnlyAccountModel(t *testing.T) {
	svc, db := newModelService(t, "http://127.0.0.1:1")
	seedAccount(t, db, "acc-reset")
	ctx := context.Background()

	model := learning.NewModel()
	for i := 0; i < 6; i++ {
		model.Train(map[string]int{"gewinn": 3}, learning.ClassSpam)
		model.Train(map[string]int{"projekt": 3}, learning.ClassHam)
	}
	if err := db.SaveLearningModel(ctx, "acc-reset", model); err != nil {
		t.Fatal(err)
	}

	cleared, err := svc.ResetLearning(ctx, "acc-reset")
	if err != nil {
		t.Fatal(err)
	}
	if cleared != 12 {
		t.Fatalf("cleared = %d, want 12 confirmed examples", cleared)
	}
	after, err := db.LoadLearningModel(ctx, "acc-reset")
	if err != nil {
		t.Fatal(err)
	}
	if after != nil && after.Trained() != 0 {
		t.Fatalf("account learning not cleared: %+v", after)
	}

	// An unknown account must error, not panic.
	if _, err := svc.ResetLearning(ctx, "does-not-exist"); err == nil {
		t.Fatal("expected error for unknown account")
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
