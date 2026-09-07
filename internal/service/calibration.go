package service

import (
	"context"

	"github.com/RHM-GER/Mailmune/internal/domain"
)

// calibrationThresholds are the score bands at which we report metrics. The
// safe auto-move threshold (0.98) is included because that is the gate the
// handoff requires before any automatic movement.
var calibrationThresholds = []float64{0.60, 0.70, 0.80, 0.90, 0.95, 0.98}

// autoMoveThreshold is the score gate for automatic moves in safe mode.
const autoMoveThreshold = 0.98

// autoMovePrecisionTarget is the minimum precision at the auto-move threshold
// before automation may be considered calibrated (handoff: 99.5%).
const autoMovePrecisionTarget = 0.995

// autoMoveMinSample is the minimum number of reviewed decisions at the
// auto-move threshold before the precision figure is trusted.
const autoMoveMinSample = 20

// CalibrationReport computes how well the filter's scores agree with human
// reviews for one account. It uses only confirmed/rejected decisions, so every
// number is real local ground truth. Recall and FPR are measured within the
// reviewed candidate population (messages below the review threshold are not
// stored and therefore not counted).
func (s *Service) CalibrationReport(ctx context.Context, accountID string) (domain.CalibrationReport, error) {
	decisions, err := s.store.ReviewedDecisions(ctx, accountID)
	if err != nil {
		return domain.CalibrationReport{}, err
	}
	report := domain.CalibrationReport{AccountID: accountID, Reviewed: len(decisions)}
	for _, decision := range decisions {
		if decision.Status == domain.StatusConfirmed {
			report.Confirmed++
		} else {
			report.Rejected++
		}
	}

	for _, threshold := range calibrationThresholds {
		var metric domain.ThresholdMetric
		metric.Threshold = threshold
		for _, decision := range decisions {
			predictedSpam := decision.Score >= threshold
			actualSpam := decision.Status == domain.StatusConfirmed
			switch {
			case predictedSpam && actualSpam:
				metric.TP++
			case predictedSpam && !actualSpam:
				metric.FP++
			case !predictedSpam && actualSpam:
				metric.FN++
			default:
				metric.TN++
			}
		}
		if metric.TP+metric.FP > 0 {
			metric.Precision = float64(metric.TP) / float64(metric.TP+metric.FP)
		}
		if metric.TP+metric.FN > 0 {
			metric.Recall = float64(metric.TP) / float64(metric.TP+metric.FN)
		}
		if metric.FP+metric.TN > 0 {
			metric.FPR = float64(metric.FP) / float64(metric.FP+metric.TN)
		}
		report.Thresholds = append(report.Thresholds, metric)

		if threshold == autoMoveThreshold {
			sample := metric.TP + metric.FP
			report.AutoMoveReady = sample >= autoMoveMinSample && metric.Precision >= autoMovePrecisionTarget
		}
	}

	report.Buckets = calibrationBuckets(decisions, 10)
	return report, nil
}

// calibrationBuckets groups reviewed decisions into equal-width score bands
// and compares the mean predicted score with the observed spam rate.
func calibrationBuckets(decisions []domain.MessageDecision, count int) []domain.CalibrationBucket {
	if count <= 0 {
		count = 10
	}
	width := 1.0 / float64(count)
	buckets := make([]domain.CalibrationBucket, count)
	for i := range buckets {
		buckets[i] = domain.CalibrationBucket{Lower: float64(i) * width, Upper: float64(i+1) * width}
	}
	sums := make([]float64, count)
	spam := make([]float64, count)
	for _, decision := range decisions {
		index := int(decision.Score / width)
		if index >= count {
			index = count - 1
		}
		if index < 0 {
			index = 0
		}
		buckets[index].Count++
		sums[index] += decision.Score
		if decision.Status == domain.StatusConfirmed {
			spam[index]++
		}
	}
	for i := range buckets {
		if buckets[i].Count > 0 {
			buckets[i].MeanScore = sums[i] / float64(buckets[i].Count)
			buckets[i].SpamRate = spam[i] / float64(buckets[i].Count)
		}
	}
	return buckets
}
