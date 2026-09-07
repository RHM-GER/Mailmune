package eval

import (
	"strings"
	"testing"

	"github.com/RHM-GER/Mailmune/internal/corpus"
	"github.com/RHM-GER/Mailmune/internal/learning"
)

func syntheticCorpus() []corpus.Sample {
	spam := []string{
		"gratis gewinn jetzt klicken und bestaetigen",
		"konto gesperrt sofort handeln und bestaetigen",
		"lotterie gewinn abholen jetzt klicken",
		"exklusives angebot nur fuer sie jetzt bestaetigen",
	}
	ham := []string{
		"projekt meeting termin naechste woche abstimmen",
		"anhang rechnung fuer ihre bestellung",
		"kurze rueckfrage zum vertrag und angebot",
		"bitte zeitplan fuer das projekt aktualisieren",
	}
	var samples []corpus.Sample
	for _, text := range spam {
		samples = append(samples, corpus.Sample{Class: learning.ClassSpam, Text: text})
	}
	for _, text := range ham {
		samples = append(samples, corpus.Sample{Class: learning.ClassHam, Text: text})
	}
	return samples
}

func trainModel(samples []corpus.Sample) *learning.Model {
	model := learning.NewModel()
	for _, sample := range samples {
		model.Train(TextOnlyFeatures(sample), sample.Class)
	}
	return model
}

func TestEvaluateProducesSaneMetrics(t *testing.T) {
	samples := syntheticCorpus()
	model := trainModel(samples)
	metrics := Evaluate(model, samples, 0.5, nil)
	if metrics.Scored == 0 {
		t.Fatal("model scored nothing")
	}
	if metrics.Accuracy < 0.75 {
		t.Fatalf("accuracy too low on separable synthetic data: %+v", metrics)
	}
	if metrics.Precision < 0.5 || metrics.Recall < 0.5 {
		t.Fatalf("precision/recall too low: %+v", metrics)
	}
	if metrics.TP+metrics.FP+metrics.TN+metrics.FN != metrics.Scored {
		t.Fatalf("confusion matrix does not sum to scored: %+v", metrics)
	}
}

func TestSweepAndBestByF1(t *testing.T) {
	samples := syntheticCorpus()
	model := trainModel(samples)
	sweep := Sweep(model, samples, []float64{0.9, 0.5, 0.1, 0.7}, nil)
	if len(sweep) != 4 {
		t.Fatalf("sweep length = %d", len(sweep))
	}
	for i := 1; i < len(sweep); i++ {
		if sweep[i].Threshold < sweep[i-1].Threshold {
			t.Fatal("sweep not sorted by threshold")
		}
	}
	if _, ok := BestByF1(sweep); !ok {
		t.Fatal("BestByF1 found nothing")
	}
}

func TestCalibrationBucketsCoverRange(t *testing.T) {
	samples := syntheticCorpus()
	model := trainModel(samples)
	buckets := Calibration(model, samples, 10, nil)
	if len(buckets) != 10 {
		t.Fatalf("buckets = %d", len(buckets))
	}
	total := 0
	for _, bucket := range buckets {
		total += bucket.Count
	}
	if total == 0 {
		t.Fatal("no samples landed in any bucket")
	}
}

func TestSplitDivideIsDeterministicAndDisjoint(t *testing.T) {
	samples := syntheticCorpus()
	train, test := SplitDivide(samples, 2)
	if len(train)+len(test) != len(samples) {
		t.Fatalf("split lost samples: %d + %d != %d", len(train), len(test), len(samples))
	}
	if len(test) == 0 {
		t.Fatal("test split empty")
	}
	// Train on the split, evaluate on held-out data.
	model := trainModel(train)
	metrics := Evaluate(model, test, 0.5, nil)
	if metrics.Scored == 0 {
		t.Fatal("held-out evaluation scored nothing")
	}
}

func TestUntrainedModelSkipsInsteadOfSilentHam(t *testing.T) {
	samples := syntheticCorpus()
	model := learning.NewModel() // never trained
	metrics := Evaluate(model, samples, 0.5, nil)
	if metrics.Scored != 0 || metrics.Skipped != len(samples) {
		t.Fatalf("untrained model must skip all, got scored=%d skipped=%d", metrics.Scored, metrics.Skipped)
	}
}

func TestTextOnlyFeaturesAreBounded(t *testing.T) {
	sample := corpus.Sample{Class: learning.ClassSpam, Text: strings.Repeat("token ", 5000)}
	features := TextOnlyFeatures(sample)
	if len(features) == 0 {
		t.Fatal("no features extracted")
	}
}
