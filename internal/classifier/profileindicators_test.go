package classifier

import (
	"testing"

	"github.com/RHM-GER/Mailmune/internal/domain"
)

var testIndicators = &domain.ProfileIndicators{
	ExpectedTopics:   []string{"design", "logo", "webseite"},
	UnexpectedTopics: []domain.UnexpectedTopic{{Name: "Diät-Kampagne", Terms: []string{"bauchfett", "abnehmen ohne diät", "keto gummies"}}},
}

// TestProfileIndicatorsOffTopicCampaign: Eine profilspezifische Fremdkampagne
// (zwei Term-Treffer) bekommt eigenen Evidence-Code und hebt den Score.
func TestProfileIndicatorsOffTopicCampaign(t *testing.T) {
	rules := NewRules()
	msg := domain.MessageFeatures{
		From: "newsletter@gesund-leben-portal.de", FromDomain: "gesund-leben-portal.de",
		Subject: "Endlich wohlfühlen",
		Text:    "Verlieren Sie Bauchfett und starten Sie abnehmen ohne Diät noch heute.",
	}
	withIndicators := rules.ClassifyFull(msg, designProfile, testIndicators, nil, nil)
	if findCode(withIndicators.Evidence, CodeProfileOffTopic) == nil {
		t.Fatalf("off-topic evidence missing: %+v", withIndicators.Evidence)
	}
	without := rules.ClassifyFull(msg, designProfile, nil, nil, nil)
	if withIndicators.Score <= without.Score {
		t.Fatalf("indicators must lift the score: with=%.2f without=%.2f", withIndicators.Score, without.Score)
	}
}

// TestProfileIndicatorsSingleShortTermDoesNotFire: Ein einzelner kurzer Treffer
// darf keine Kampagnenkategorie auslösen (Doppel-Absicherung gegen zu breite
// Kompilate).
func TestProfileIndicatorsSingleShortTermDoesNotFire(t *testing.T) {
	rules := NewRules()
	msg := domain.MessageFeatures{
		From: "redaktion@fitness-magazin.de", FromDomain: "fitness-magazin.de",
		Subject: "Trainingstipps",
		Text:    "Bauchfett wird oft unterschätzt.",
	}
	classification := rules.ClassifyFull(msg, designProfile, testIndicators, nil, nil)
	if findCode(classification.Evidence, CodeProfileOffTopic) != nil {
		t.Fatalf("single short term must not fire: %+v", classification.Evidence)
	}
}

// TestProfileIndicatorsExpectedTopicsReduceScore: Legitime Themen des Profils
// (zwei Treffer) senken den Score als eigenes Negativ-Signal.
func TestProfileIndicatorsExpectedTopicsReduceScore(t *testing.T) {
	rules := NewRules()
	msg := domain.MessageFeatures{
		From: "kunde@mittelstand.example", FromDomain: "kunde@mittelstand.example",
		Subject: "Neues Logo für die Webseite",
		Text:    "Anbei der Entwurf für das Logo und die neue Webseite. Bitte kurzes Feedback zum Design.",
	}
	classification := rules.ClassifyFull(msg, designProfile, testIndicators, nil, nil)
	if findCode(classification.Evidence, CodeProfileTopicMatch) == nil {
		t.Fatalf("topic-match evidence missing: %+v", classification.Evidence)
	}
	without := rules.ClassifyFull(msg, designProfile, nil, nil, nil)
	if classification.Score >= without.Score {
		t.Fatalf("expected-topic match must lower the score: with=%.2f without=%.2f", classification.Score, without.Score)
	}
}

// TestProfileIndicatorsNilSafe: Ohne Kompilat verhält sich die Pipeline exakt
// wie vorher.
func TestProfileIndicatorsNilSafe(t *testing.T) {
	rules := NewRules()
	msg := domain.MessageFeatures{
		From: "kunde@mittelstand.example", FromDomain: "kunde.example",
		Subject: "Rechnung 2026-114",
		Text:    "Anbei die freigegebene Rechnung.",
	}
	withNil := rules.ClassifyFull(msg, designProfile, nil, nil, nil)
	legacy := rules.ClassifyWithFeatures(msg, designProfile, nil, nil)
	if withNil.Score != legacy.Score {
		t.Fatalf("nil indicators must not change scoring: %v vs %v", withNil.Score, legacy.Score)
	}
}
