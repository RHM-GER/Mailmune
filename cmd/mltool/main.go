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
	default:
		usage()
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `mltool - offline ML utilities for Mailmune

Usage:
  mltool eval   <corpus.csv> [--max N] [--thresholds 0.4,0.5,0.6,0.7,0.8]
  mltool import <mailmune.db> <corpus.csv> --source "name/url" --license MIT [--max N]

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
	sweep := eval.Sweep(model, test, parsed, eval.TextOnlyFeatures)
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
	for _, bucket := range eval.Calibration(model, test, 10, eval.TextOnlyFeatures) {
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
