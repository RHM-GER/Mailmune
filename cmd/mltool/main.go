// Command mltool provides offline, read-only machine-learning utilities:
//
//	mltool eval   <corpus.csv>                         benchmark the text learner
//	mltool import <db> <corpus.csv> --source S --license L
//	                                                   train and store the opt-in baseline
//
// Nothing is downloaded; the user points the tool at a local, licensed corpus.
// The learner stays fully local and explainable.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/RHM-GER/Mailmune/internal/corpus"
	"github.com/RHM-GER/Mailmune/internal/domain"
	"github.com/RHM-GER/Mailmune/internal/provider"
	"github.com/RHM-GER/Mailmune/internal/eval"
	"github.com/RHM-GER/Mailmune/internal/learning"
	"github.com/RHM-GER/Mailmune/internal/service"
	"github.com/RHM-GER/Mailmune/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "eval":
		evalCommand(os.Args[2:])
	case "import":
		importCommand(os.Args[2:])
	case "embed":
		embedCommand(os.Args[2:])
	default:
		usage()
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `mltool - offline ML utilities for Mailmune

Usage:
  mltool eval   <corpus.csv> [--max N] [--thresholds 0.4,0.5,0.6,0.7,0.8]
  mltool import <mailmune.db> <corpus.csv> --source "name/url" --license MIT [--max N]
  mltool embed  <mailmune.db> <corpus.csv> --model qwen3-embedding:0.6b \
                --source "name/url" --license MIT [--max N] [--batch 16] [--ollama URL]

Corpus: CSV with a header and a label + text column.
Labels: 0/ham -> ham, 1/2/spam/phish -> spam.
`)
	os.Exit(2)
}

func evalCommand(args []string) {
	flags := flag.NewFlagSet("eval", flag.ExitOnError)
	maxRows := flags.Int("max", 0, "limit number of rows (0 = all)")
	thresholds := flags.String("thresholds", "0.4,0.5,0.6,0.7,0.8,0.9", "comma-separated thresholds to sweep")
	_ = flags.Parse(args)
	if flags.NArg() < 1 {
		usage()
	}
	path := flags.Arg(0)

	samples, err := corpus.LoadFile(path, corpus.Options{MaxRows: *maxRows})
	if err != nil {
		log.Fatal(err)
	}
	counts := corpus.Count(samples)
	fmt.Printf("Korpus: %s\n", path)
	fmt.Printf("Zeilen: %d (Spam %d / Ham %d)\n\n", counts.Total, counts.Spam, counts.Ham)

	train, test := eval.SplitDivide(samples, 5)
	fmt.Printf("Train %d · Test (gehalten) %d\n", len(train), len(test))

	model := learning.NewModel()
	start := time.Now()
	for _, sample := range train {
		model.Train(eval.TextOnlyFeatures(sample), sample.Class)
	}
	fmt.Printf("Training: %s\n\n", time.Since(start).Round(time.Millisecond))

	parsed := parseThresholds(*thresholds)
	// Features einmal extrahieren statt pro Schwelle – der Sweep über große
	// Holdouts wird damit um den Faktor Anzahl-Schwellen schneller.
	feats := eval.ExtractAll(test, eval.TextOnlyFeatures)
	sweep := eval.SweepPrecomputed(model, test, parsed, feats)
	fmt.Printf("%-9s %-9s %-9s %-9s %-9s %-9s %s\n", "THRESH", "PREC", "RECALL", "FPR", "F1", "ACC", "TP/FP/TN/FN")
	for _, metrics := range sweep {
		fmt.Printf("%-9.2f %-9.3f %-9.3f %-9.3f %-9.3f %-9.3f %d/%d/%d/%d\n",
			metrics.Threshold, metrics.Precision, metrics.Recall, metrics.FPR, metrics.F1, metrics.Accuracy,
			metrics.TP, metrics.FP, metrics.TN, metrics.FN)
	}
	if best, ok := eval.BestByF1(sweep); ok {
		fmt.Printf("\nBester F1 bei Threshold %.2f: F1=%.3f Precision=%.3f Recall=%.3f FPR=%.3f\n",
			best.Threshold, best.F1, best.Precision, best.Recall, best.FPR)
	}

	fmt.Printf("\nKalibrierung (10 Buckets):\n%-12s %-7s %-9s %s\n", "BEREICH", "ANZAHL", "MITTEL", "SPAM-RATE")
	for _, bucket := range eval.CalibrationPrecomputed(model, test, 10, feats) {
		if bucket.Count == 0 {
			continue
		}
		fmt.Printf("%-12s %-7d %-9.3f %.3f\n", fmt.Sprintf("%.1f-%.1f", bucket.Lower, bucket.Upper), bucket.Count, bucket.MeanScore, bucket.SpamRate)
	}
}

func importCommand(args []string) {
	flags := flag.NewFlagSet("import", flag.ExitOnError)
	source := flags.String("source", "", "provenance of the corpus (name or URL); required")
	license := flags.String("license", "", "license of the corpus (e.g. MIT); required")
	maxRows := flags.Int("max", 0, "limit number of rows (0 = all)")
	_ = flags.Parse(args)
	if flags.NArg() < 2 || *source == "" || *license == "" {
		usage()
	}
	dbPath, corpusPath := flags.Arg(0), flags.Arg(1)

	file, err := os.Open(corpusPath)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	db, err := store.Open(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// The import path only touches the baseline tables; mailbox/secrets are
	// unused here, so a minimal service is enough.
	svc := service.NewForBaseline(db)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	result, err := svc.ImportBaseline(ctx, file, service.BaselineImportRequest{Source: *source, License: *license, MaxRows: *maxRows})
	if err != nil {
		log.Fatal(err)
	}
	m := result.MetricsOnHoldout
	fmt.Printf("Baseline importiert: id=%s version=%d\n", result.Meta.ID, result.Meta.Version)
	fmt.Printf("Herkunft: %s · Lizenz: %s · Zeilen: %d\n", result.Meta.Source, result.Meta.License, result.Meta.CorpusRows)
	fmt.Printf("Trainiert: %d Beispiele (Spam %d / Ham %d)\n", result.Trained, result.Meta.SpamMessages, result.Meta.HamMessages)
	fmt.Printf("Holdout @0.5: Precision=%.3f Recall=%.3f FPR=%.3f F1=%.3f Accuracy=%.3f (TP/FP/TN/FN %d/%d/%d/%d)\n",
		m.Precision, m.Recall, m.FPR, m.F1, m.Accuracy, m.TP, m.FP, m.TN, m.FN)
	fmt.Println("\nDie Baseline wirkt als begrenzter Prior; dein bestätigtes Lernen kann sie überstimmen.")
}

func parseThresholds(raw string) []float64 {
	var out []float64
	for _, part := range strings.Split(raw, ",") {
		var value float64
		if _, err := fmt.Sscan(strings.TrimSpace(part), &value); err == nil {
			out = append(out, value)
		}
	}
	if len(out) == 0 {
		out = []float64{0.5}
	}
	return out
}

// embedCommand berechnet die Spam-/Ham-Zentroide eines Embedding-Modells
// (z. B. qwen3-embedding:0.6b) aus einem lokalen Korpus und legt sie in der
// Agent-Datenbank ab. Die Vektoren kommen gebatcht von Ollama /api/embed;
// es wird nichts hochgeladen und nichts generiert.
func embedCommand(args []string) {
	flags := flag.NewFlagSet("embed", flag.ExitOnError)
	model := flags.String("model", "", "Embedding-Modell in Ollama (z. B. qwen3-embedding:0.6b); Pflicht")
	ollamaURL := flags.String("ollama", "http://127.0.0.1:11434", "Ollama-Basis-URL")
	batchSize := flags.Int("batch", 16, "Eingaben pro /api/embed-Aufruf")
	maxRows := flags.Int("max", 20000, "maximale Samples insgesamt (balanciert über beide Klassen)")
	source := flags.String("source", "", "Herkunft des Korpus (Name/URL); Pflicht")
	license := flags.String("license", "", "Lizenz des Korpus (z. B. MIT); Pflicht")
	_ = flags.Parse(args)
	if flags.NArg() < 2 || *model == "" || *source == "" || *license == "" || *batchSize < 1 || *maxRows < 200 {
		usage()
	}
	dbPath, corpusPath := flags.Arg(0), flags.Arg(1)

	samples, err := corpus.LoadFile(corpusPath, corpus.Options{MaxRows: *maxRows * 2})
	if err != nil {
		log.Fatal(err)
	}
	var spam, ham []corpus.Sample
	for _, sample := range samples {
		if sample.Class == learning.ClassSpam {
			spam = append(spam, sample)
		} else {
			ham = append(ham, sample)
		}
	}
	perClass := *maxRows / 2
	if len(spam) > perClass {
		spam = spam[:perClass]
	}
	if len(ham) > perClass {
		ham = ham[:perClass]
	}
	if len(spam) < 100 || len(ham) < 100 {
		log.Fatalf("zu wenige Samples pro Klasse (spam=%d ham=%d)", len(spam), len(ham))
	}

	client, err := provider.NewOllama(*ollamaURL)
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	centerOf := func(label string, set []corpus.Sample) ([]float64, error) {
		var center []float64
		count := 0
		for start := 0; start < len(set); start += *batchSize {
			end := start + *batchSize
			if end > len(set) {
				end = len(set)
			}
			texts := make([]string, 0, end-start)
			for _, sample := range set[start:end] {
				text := sample.Text
				if len(text) > 8000 {
					text = text[:8000]
				}
				texts = append(texts, text)
			}
			vectors, err := client.Embed(ctx, *model, texts)
			if err != nil {
				return nil, err
			}
			for _, vector := range vectors {
				if center == nil {
					center = make([]float64, len(vector))
				}
				if len(vector) != len(center) {
					return nil, fmt.Errorf("inkonsistente Vektorlängen vom Modell %q", *model)
				}
				for i, value := range vector {
					center[i] += value
				}
				count++
			}
			fmt.Printf("\r%s: %d/%d Vektoren ", label, count, len(set))
		}
		fmt.Println()
		if count == 0 || center == nil {
			return nil, fmt.Errorf("keine Vektoren für %s erhalten", label)
		}
		for i := range center {
			center[i] /= float64(count)
		}
		return center, nil
	}

	spamCenter, err := centerOf("Spam", spam)
	if err != nil {
		log.Fatal(err)
	}
	hamCenter, err := centerOf("Ham", ham)
	if err != nil {
		log.Fatal(err)
	}

	db, err := store.Open(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	record := domain.EmbeddingCentroids{
		Model: *model, Dim: len(spamCenter),
		SpamCenter: spamCenter, HamCenter: hamCenter,
		SpamN: len(spam), HamN: len(ham),
		Source: *source, License: *license, CreatedAt: time.Now().UTC(),
	}
	if err := db.SaveEmbeddingCentroids(ctx, record); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Zentroide gespeichert: model=%s dim=%d spam=%d ham=%d (%s, %s)\n", record.Model, record.Dim, record.SpamN, record.HamN, record.Source, record.License)
	fmt.Println("Fast-Pfad aktiv, sobald im Postfach dieses Embedding-Modell gewählt ist.")
}
