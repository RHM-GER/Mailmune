// Package eval measures the quality of the local statistical learner against
// a labeled corpus. It reports the standard classification metrics and a
// calibration view so threshold and model choices become evidence-based
// instead of guesswork.
//
// Evaluation uses only the message text: public corpora carry no reliable
// sender/domain metadata, so the deterministic sender heuristics are out of
// scope here. The numbers therefore describe the text learner alone.
package eval

import (
	"math"
	"sort"

	"github.com/RHM-GER/Mailmune/internal/corpus"
	"github.com/RHM-GER/Mailmune/internal/learning"
)

// FeatureFunc turns a sample into the token features the learner consumes.
type FeatureFunc func(corpus.Sample) map[string]int

// TextOnlyFeatures extracts features from the sample text, which is all a
// public corpus provides.
func TextOnlyFeatures(sample corpus.Sample) map[string]int {
	return learning.ExtractFeatures("", "", "", sample.Text)
}

// Metrics is the confusion-matrix summary at one decision threshold.
type Metrics struct {
	Threshold float64
	TP        int
	FP        int
	TN        int
	FN        int
	Precision float64
	Recall    float64
	FPR       float64
	F1        float64
	Accuracy  float64
	Scored    int
	Skipped   int
}

// Evaluate scores every sample with the model and classifies it as spam when
// the score meets the threshold. Samples the model refuses to score (too few
// confirmed examples) are counted as skipped, never as silent ham.
func Evaluate(model *learning.Model, samples []corpus.Sample, threshold float64, features FeatureFunc) Metrics {
	if features == nil {
		features = TextOnlyFeatures
	}
	metrics := Metrics{Threshold: threshold}
	for _, sample := range samples {
		score, ok := model.Score(features(sample))
		if !ok {
			metrics.Skipped++
			continue
		}
		metrics.Scored++
		predictedSpam := score >= threshold
		actualSpam := sample.Class == learning.ClassSpam
		switch {
		case predictedSpam && actualSpam:
			metrics.TP++
		case predictedSpam && !actualSpam:
			metrics.FP++
		case !predictedSpam && actualSpam:
			metrics.FN++
		default:
			metrics.TN++
		}
	}
	metrics.compute()
	return metrics
}

func (m *Metrics) compute() {
	if m.TP+m.FP > 0 {
		m.Precision = float64(m.TP) / float64(m.TP+m.FP)
	}
	if m.TP+m.FN > 0 {
		m.Recall = float64(m.TP) / float64(m.TP+m.FN)
	}
	if m.FP+m.TN > 0 {
		m.FPR = float64(m.FP) / float64(m.FP+m.TN)
	}
	if m.Precision+m.Recall > 0 {
		m.F1 = 2 * m.Precision * m.Recall / (m.Precision + m.Recall)
	}
	total := m.TP + m.FP + m.TN + m.FN
	if total > 0 {
		m.Accuracy = float64(m.TP+m.TN) / float64(total)
	}
}

// Sweep evaluates a range of thresholds and returns the metrics for each,
// sorted by ascending threshold.
func Sweep(model *learning.Model, samples []corpus.Sample, thresholds []float64, features FeatureFunc) []Metrics {
	sorted := append([]float64(nil), thresholds...)
	sort.Float64s(sorted)
	results := make([]Metrics, 0, len(sorted))
	for _, threshold := range sorted {
		results = append(results, Evaluate(model, samples, threshold, features))
	}
	return results
}

// BestByF1 returns the sweep entry with the highest F1 score.
func BestByF1(sweep []Metrics) (Metrics, bool) {
	var best Metrics
	found := false
	for _, metrics := range sweep {
		if !found || metrics.F1 > best.F1 {
			best = metrics
			found = true
		}
	}
	return best, found
}

// CalibrationBucket reports how often samples in a predicted-score band were
// actually spam. A well-calibrated model has MeanScore close to SpamRate.
type CalibrationBucket struct {
	Lower    float64
	Upper    float64
	Count    int
	MeanScore float64
	SpamRate float64
}

// Calibration groups scored samples into equal-width buckets and compares the
// mean predicted score with the observed spam rate per bucket.
func Calibration(model *learning.Model, samples []corpus.Sample, buckets int, features FeatureFunc) []CalibrationBucket {
	if features == nil {
		features = TextOnlyFeatures
	}
	if buckets <= 0 {
		buckets = 10
	}
	width := 1.0 / float64(buckets)
	out := make([]CalibrationBucket, buckets)
	for i := range out {
		out[i] = CalibrationBucket{Lower: float64(i) * width, Upper: float64(i+1) * width}
	}
	for _, sample := range samples {
		score, ok := model.Score(features(sample))
		if !ok {
			continue
		}
		index := int(math.Floor(score / width))
		if index >= buckets {
			index = buckets - 1
		}
		if index < 0 {
			index = 0
		}
		out[index].Count++
		out[index].MeanScore += score
		if sample.Class == learning.ClassSpam {
			out[index].SpamRate += 1
		}
	}
	for i := range out {
		if out[i].Count > 0 {
			out[i].MeanScore /= float64(out[i].Count)
			out[i].SpamRate /= float64(out[i].Count)
		}
	}
	return out
}

// SplitDivide deterministically splits samples into a training and a held-out
// test set: every k-th sample (by position) goes to test. This keeps the split
// reproducible without a random seed.
func SplitDivide(samples []corpus.Sample, testEvery int) (train, test []corpus.Sample) {
	if testEvery < 2 {
		testEvery = 5
	}
	for i, sample := range samples {
		if (i+1)%testEvery == 0 {
			test = append(test, sample)
		} else {
			train = append(train, sample)
		}
	}
	return train, test
}
