package service

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/RHM-GER/Mailmune/internal/corpus"
	"github.com/RHM-GER/Mailmune/internal/domain"
	"github.com/RHM-GER/Mailmune/internal/eval"
	"github.com/RHM-GER/Mailmune/internal/learning"
)

// BaselineID is the singleton identifier of the optional global learner.
const BaselineID = "global"

// BaselineImportRequest describes an opt-in import of an external corpus.
// Source and License are mandatory provenance and are stored verbatim so the
// user can always see where the global knowledge came from and under which
// terms.
type BaselineImportRequest struct {
	Source  string
	License string
	// MaxRows bounds the import; 0 reads the whole corpus.
	MaxRows int
}

// BaselineImportResult reports what was imported and how it performs on a
// deterministic held-out split, so the user sees the value before relying on it.
type BaselineImportResult struct {
	Meta    domain.LearningBaseline
	Trained uint64
	// MetricsOnHoldout is measured on a deterministic held-out slice of the
	// same corpus at the review threshold.
	MetricsOnHoldout eval.Metrics
}

// ImportBaseline trains a global baseline learner from a labeled corpus and
// persists it with provenance. It never touches per-account confirmed
// learning. The corpus is read from r and not retained beyond training.
func (s *Service) ImportBaseline(ctx context.Context, r io.Reader, request BaselineImportRequest) (BaselineImportResult, error) {
	if request.Source == "" || request.License == "" {
		return BaselineImportResult{}, errors.New("source and license are required for an imported baseline")
	}
	samples, err := corpus.Load(r, corpus.Options{MaxRows: request.MaxRows})
	if err != nil {
		return BaselineImportResult{}, fmt.Errorf("load corpus: %w", err)
	}

	// Deterministic split: train the shipped baseline on the train part and
	// report honest metrics on the held-out part.
	train, test := eval.SplitDivide(samples, 5)
	model := learning.NewModel()
	for _, sample := range train {
		model.Train(eval.TextOnlyFeatures(sample), sample.Class)
	}
	metrics := eval.Evaluate(model, test, 0.5, eval.TextOnlyFeatures)

	meta := domain.LearningBaseline{
		ID:         BaselineID,
		Version:    learning.Version,
		Source:     request.Source,
		License:    request.License,
		CorpusRows: len(samples),
	}
	if err := s.store.SaveBaseline(ctx, meta, model); err != nil {
		return BaselineImportResult{}, err
	}
	saved, savedMeta, _, err := s.store.LoadBaseline(ctx, BaselineID)
	if err != nil {
		return BaselineImportResult{}, err
	}
	if s.hub != nil {
		s.hub.Publish("baseline.updated", savedMeta)
	}
	return BaselineImportResult{Meta: savedMeta, Trained: saved.Trained(), MetricsOnHoldout: metrics}, nil
}

// Baselines lists stored global baselines with provenance.
func (s *Service) Baselines(ctx context.Context) ([]domain.LearningBaseline, error) {
	return s.store.ListBaselines(ctx)
}

// DeleteBaseline removes the global baseline. Confirmed per-account learning
// is never affected.
func (s *Service) DeleteBaseline(ctx context.Context, id string) error {
	if id == "" {
		id = BaselineID
	}
	if err := s.store.DeleteBaseline(ctx, id); err != nil {
		return err
	}
	if s.hub != nil {
		s.hub.Publish("baseline.deleted", map[string]string{"id": id})
	}
	return nil
}
