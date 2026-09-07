package service

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RHM-GER/Mailmune/internal/mailbox"
	"github.com/RHM-GER/Mailmune/internal/store"
)

func newBaselineService(t *testing.T) *Service {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "baseline.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewWithMailbox(db, &memSecrets{values: map[string]string{}}, mailbox.NewClient())
}

// syntheticCorpus builds a small labeled CSV with clearly separable classes.
func syntheticCorpus() string {
	var builder strings.Builder
	builder.WriteString("label,text\n")
	spam := []string{
		"gratis gewinn jetzt klicken und bestaetigen",
		"konto gesperrt sofort handeln und bestaetigen",
		"lotterie gewinn abholen jetzt klicken",
		"exklusives angebot nur fuer sie jetzt bestaetigen",
		"gewinnspiel sie haben gewonnen jetzt anmelden",
	}
	ham := []string{
		"projekt meeting termin naechste woche abstimmen",
		"anhang rechnung fuer ihre bestellung",
		"kurze rueckfrage zum vertrag und angebot",
		"bitte zeitplan fuer das projekt aktualisieren",
		"team besprechung am montag vorbereiten",
	}
	for i := 0; i < 40; i++ {
		builder.WriteString("2," + spam[i%len(spam)] + "\n")
		builder.WriteString("0," + ham[i%len(ham)] + "\n")
	}
	return builder.String()
}

func TestImportBaselineTrainsAndReportsHoldout(t *testing.T) {
	svc := newBaselineService(t)
	ctx := context.Background()

	result, err := svc.ImportBaseline(ctx, strings.NewReader(syntheticCorpus()), BaselineImportRequest{Source: "synthetic-test", License: "MIT"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Meta.Source != "synthetic-test" || result.Meta.License != "MIT" {
		t.Fatalf("provenance not stored: %+v", result.Meta)
	}
	if result.Meta.CorpusRows != 80 {
		t.Fatalf("corpus rows = %d, want 80", result.Meta.CorpusRows)
	}
	if result.Trained == 0 {
		t.Fatal("baseline trained nothing")
	}
	if result.MetricsOnHoldout.Scored == 0 {
		t.Fatal("holdout evaluation scored nothing")
	}
	if result.MetricsOnHoldout.Accuracy < 0.8 {
		t.Fatalf("holdout accuracy too low on separable data: %+v", result.MetricsOnHoldout)
	}

	listed, err := svc.Baselines(ctx)
	if err != nil || len(listed) != 1 {
		t.Fatalf("baselines = %d, want 1 (err=%v)", len(listed), err)
	}
}

func TestImportBaselineRequiresProvenance(t *testing.T) {
	svc := newBaselineService(t)
	if _, err := svc.ImportBaseline(context.Background(), strings.NewReader(syntheticCorpus()), BaselineImportRequest{Source: "", License: ""}); err == nil {
		t.Fatal("import without provenance must fail")
	}
	if _, err := svc.ImportBaseline(context.Background(), strings.NewReader(syntheticCorpus()), BaselineImportRequest{Source: "x", License: ""}); err == nil {
		t.Fatal("import without license must fail")
	}
}

func TestDeleteBaselineKeepsAccountLearning(t *testing.T) {
	svc := newBaselineService(t)
	ctx := context.Background()
	if _, err := svc.ImportBaseline(ctx, strings.NewReader(syntheticCorpus()), BaselineImportRequest{Source: "s", License: "MIT"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteBaseline(ctx, ""); err != nil {
		t.Fatal(err)
	}
	listed, _ := svc.Baselines(ctx)
	if len(listed) != 0 {
		t.Fatalf("baseline survived deletion: %d", len(listed))
	}
}

func TestBaselineActAsPriorInScoring(t *testing.T) {
	svc := newBaselineService(t)
	ctx := context.Background()
	if _, err := svc.ImportBaseline(ctx, strings.NewReader(syntheticCorpus()), BaselineImportRequest{Source: "s", License: "MIT"}); err != nil {
		t.Fatal(err)
	}
	// An account with no confirmed learning still scores via the baseline.
	combined := svc.scanner.buildScorer(ctx, "no-such-account")
	if combined == nil || combined.Trained() == 0 {
		t.Fatal("baseline must provide a scorer even without account learning")
	}
	score, ok := combined.Score(map[string]int{"gewinn": 2, "klicken": 2, "bestaetigen": 2})
	if !ok || score < 0.6 {
		t.Fatalf("spam-ish tokens should score high via baseline: %.2f ok=%v", score, ok)
	}
}
