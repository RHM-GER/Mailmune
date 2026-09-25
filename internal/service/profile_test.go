package service

import (
	"context"
	"testing"
	"time"

	"github.com/RHM-GER/Mailmune/internal/domain"
	"github.com/RHM-GER/Mailmune/internal/mailbox/imaptest"
	"github.com/RHM-GER/Mailmune/internal/provider"
)

// compileJSON ist eine minimale, gültige Kompilat-Antwort des Fake-Ollama.
const compileJSON = `{"prompt":"Design-Agentur erwartet Kundenpost; fremd sind Diaet- und Krypto-Kampagnen.","expectedTopics":["design","logo","webseite"],"unexpectedTopics":[{"name":"Diaet","terms":["bauchfett","abnehmen ohne"]}],"notes":"ok"}`

func TestCompileAccountProfileStoresValidatedModel(t *testing.T) {
	fake := fakeOllamaServer(t, compileJSON)
	svc, db := newModelService(t, fake.URL)
	seedAccount(t, db, "prof-1")
	ctx := context.Background()

	account, err := db.Account(ctx, "prof-1")
	if err != nil {
		t.Fatal(err)
	}
	account.OllamaModel = "qwen3:4b-instruct-2507"
	account.OllamaValidated = true
	account.Profile = domain.MailboxProfile{
		Purpose: "Posteingang einer Design-Agentur",
		Context: "Keine Produktwerbung, keine Kaltakquise",
	}
	if err := db.UpsertAccount(ctx, account); err != nil {
		t.Fatal(err)
	}

	model, err := svc.CompileAccountProfile(ctx, "prof-1")
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if model.Prompt == "" || len(model.Indicators.ExpectedTopics) != 3 || len(model.Indicators.UnexpectedTopics) != 1 {
		t.Fatalf("compiled model wrong: %+v", model)
	}
	if !model.Enabled || model.SourceHash == "" || model.Model != account.OllamaModel {
		t.Fatalf("compiled metadata wrong: %+v", model)
	}

	loaded, found, stale, err := svc.AccountProfileModel(ctx, "prof-1")
	if err != nil || !found || stale {
		t.Fatalf("fresh compilation must be found and not stale: %v %v %v", found, stale, err)
	}
	if loaded.Prompt != model.Prompt {
		t.Fatal("stored prompt differs")
	}

	// Profiltext geändert -> Kompilat veraltet.
	account.Profile.Purpose += " und Videoproduktion"
	if err := db.UpsertAccount(ctx, account); err != nil {
		t.Fatal(err)
	}
	if _, _, stale, err := svc.AccountProfileModel(ctx, "prof-1"); err != nil || !stale {
		t.Fatalf("changed profile must mark compilation stale: stale=%v err=%v", stale, err)
	}

	// Deaktivieren löscht das Kompilat nicht.
	if err := svc.SetProfileModelEnabled(ctx, "prof-1", false); err != nil {
		t.Fatal(err)
	}
	if loaded, found, _, err := svc.AccountProfileModel(ctx, "prof-1"); err != nil || !found || loaded.Enabled {
		t.Fatalf("disabled model must stay stored but disabled: %+v %v %v", loaded, found, err)
	}
}

func TestCompileAccountProfileRequiresValidatedModel(t *testing.T) {
	fake := fakeOllamaServer(t, compileJSON)
	svc, db := newModelService(t, fake.URL)
	seedAccount(t, db, "prof-2")
	ctx := context.Background()

	account, err := db.Account(ctx, "prof-2")
	if err != nil {
		t.Fatal(err)
	}
	account.Profile = domain.MailboxProfile{Purpose: "Irgendein Zweck"}
	if err := db.UpsertAccount(ctx, account); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CompileAccountProfile(ctx, "prof-2"); err == nil {
		t.Fatal("compilation without validated model must fail")
	}

	account.OllamaModel = "qwen3:4b-instruct-2507"
	account.OllamaValidated = true
	account.Profile = domain.MailboxProfile{} // zu leer
	if err := db.UpsertAccount(ctx, account); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CompileAccountProfile(ctx, "prof-2"); err == nil {
		t.Fatal("compilation of an empty profile must fail")
	}
}

// TestStaleProfileModelAutoRecompilesAfterScan: Ein veraltetes Kompilat bleibt
// nicht still ungenutzt – nach dem nächsten abgeschlossenen Scan kompiliert der
// Scanner es im Hintergrund neu (Hash passt danach zum Profiltext).
func TestStaleProfileModelAutoRecompilesAfterScan(t *testing.T) {
	fake := fakeOllamaServer(t, compileJSON)
	server := imaptest.New(t, rev2Caps())
	svc, db := newTestService(t, server)
	ollama, err := provider.NewOllama(fake.URL)
	if err != nil {
		t.Fatal(err)
	}
	svc.ollama = ollama
	svc.scanner.ollama = ollama
	account := createTestAccount(t, svc, server, "prof-auto")
	ctx := context.Background()

	account.OllamaModel = "qwen3:4b-instruct-2507"
	account.OllamaValidated = true
	account.AIEnabled = true
	account.Profile = domain.MailboxProfile{Purpose: "Design-Agentur", Context: "keine Produktwerbung"}
	if err := db.UpsertAccount(ctx, account); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CompileAccountProfile(ctx, "prof-auto"); err != nil {
		t.Fatal(err)
	}

	account.Profile.Context = "keine Produktwerbung, keine Kaltakquise"
	if err := db.UpsertAccount(ctx, account); err != nil {
		t.Fatal(err)
	}
	if _, _, stale, _ := svc.AccountProfileModel(ctx, "prof-auto"); !stale {
		t.Fatal("compilation must be stale after profile edit")
	}

	if _, err := svc.StartScan(ctx, account.ID, true); err != nil {
		t.Fatal(err)
	}
	if run := waitForScan(t, svc, account.ID); run.Status != domain.ScanCompleted {
		t.Fatalf("run: %s (%s)", run.Status, run.Error)
	}

	want := ProfileSourceHash(account.Profile)
	for attempt := 0; attempt < 50; attempt++ {
		model, found, stale, err := svc.AccountProfileModel(ctx, "prof-auto")
		if err == nil && found && !stale && model.SourceHash == want {
			return // automatisch rekompiliert
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("stale compilation was not auto-recompiled after the scan")
}

func TestProfileSourceHashStableAndSensitive(t *testing.T) {
	base := domain.MailboxProfile{Purpose: " Design-Agentur ", Industry: "Medien", ExpectedMailTypes: []string{"Kundenanfragen", "Rechnungen"}}
	same := domain.MailboxProfile{Purpose: "Design-Agentur", Industry: "Medien", ExpectedMailTypes: []string{"Rechnungen", "Kundenanfragen"}}
	if ProfileSourceHash(base) != ProfileSourceHash(same) {
		t.Fatal("hash must be order/whitespace insensitive")
	}
	changed := same
	changed.Context = "Neu"
	if ProfileSourceHash(base) == ProfileSourceHash(changed) {
		t.Fatal("hash must react to context changes")
	}
}
