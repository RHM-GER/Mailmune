package classifier

import (
	"testing"

	"github.com/RHM-GER/Mailmune/internal/domain"
)

func findEvidence(evidence []domain.Evidence, code string) *domain.Evidence {
	for i := range evidence {
		if evidence[i].Code == code {
			return &evidence[i]
		}
	}
	return nil
}

func TestDenyRulesProduceStableCodes(t *testing.T) {
	rules := NewRules()
	msg := domain.MessageFeatures{From: "boss@mafia.example", FromDomain: "mafia.example", Subject: "Sonderangebot Krypto-Investment", Text: "casino bonus jetzt"}
	profile := domain.MailboxProfile{
		DeniedSenders:  []string{"boss@mafia.example"},
		DeniedDomains:  []string{"casino.example"},
		DeniedKeywords: []string{"Krypto-Investment"},
	}
	result := rules.Classify(msg, profile)
	for _, code := range []string{CodeDenySender, CodeDenyKeyword} {
		if findEvidence(result.Evidence, code) == nil {
			t.Fatalf("evidence %s missing: %+v", code, result.Evidence)
		}
	}
	if findEvidence(result.Evidence, CodeDenyDomain) != nil {
		t.Fatal("deny_domain must not fire for a different domain")
	}
	if result.Score < 0.6 {
		t.Fatalf("deny signals must reach review threshold, got %.2f", result.Score)
	}
}

func TestAllowRulesCreateStrongTrust(t *testing.T) {
	rules := NewRules()
	msg := domain.MessageFeatures{From: "chef@firma.example", FromDomain: "firma.example", Subject: "Gewinn sofort handeln", Text: "gewinn lotterie"}
	profile := domain.MailboxProfile{TrustedDomains: []string{"firma.example"}}
	result := rules.Classify(msg, profile)
	if !result.StrongTrustSignal {
		t.Fatal("trusted domain must create a strong trust signal")
	}
	if findEvidence(result.Evidence, CodeAllowDomain) == nil {
		t.Fatalf("allow_domain evidence missing: %+v", result.Evidence)
	}
	if result.Score > 0.79 {
		t.Fatalf("trusted sender score must stay capped, got %.2f", result.Score)
	}
	if action := Decide(domain.SafetySafe, result); action == ActionMove {
		t.Fatal("trusted correspondent must never be moved automatically")
	}
}

type fixedScorer struct {
	probability float64
	ok          bool
}

func (f fixedScorer) Score(map[string]int) (float64, bool) { return f.probability, f.ok }

func TestStatisticalEvidenceCountsAsOneGroup(t *testing.T) {
	rules := NewRules()
	msg := domain.MessageFeatures{From: "unbekannt@example.com", FromDomain: "example.com", Subject: "neutral"}
	profile := domain.MailboxProfile{}

	spammy := rules.ClassifyWithFeatures(msg, profile, map[string]int{"token": 1}, fixedScorer{probability: 0.95, ok: true})
	item := findEvidence(spammy.Evidence, CodeStatisticalSpam)
	if item == nil {
		t.Fatalf("statistical_spam evidence missing: %+v", spammy.Evidence)
	}
	if spammy.IndependentGroups != 1 {
		t.Fatalf("statistical signal alone must be one group, got %d", spammy.IndependentGroups)
	}
	if Decide(domain.SafetySafe, spammy) != ActionQueue {
		t.Fatal("a single statistical signal must never auto-move")
	}

	hammy := rules.ClassifyWithFeatures(msg, profile, map[string]int{"token": 1}, fixedScorer{probability: 0.05, ok: true})
	if findEvidence(hammy.Evidence, CodeStatisticalHam) == nil {
		t.Fatalf("statistical_ham evidence missing: %+v", hammy.Evidence)
	}

	uncertain := rules.ClassifyWithFeatures(msg, profile, map[string]int{"token": 1}, fixedScorer{probability: 0.5, ok: true})
	if len(uncertain.Evidence) != 0 {
		t.Fatalf("uncertain model must not contribute evidence: %+v", uncertain.Evidence)
	}

	untrained := rules.ClassifyWithFeatures(msg, profile, map[string]int{"token": 1}, fixedScorer{ok: false})
	if len(untrained.Evidence) != 0 {
		t.Fatalf("untrained model must not contribute evidence: %+v", untrained.Evidence)
	}
}

func TestBaselineWithoutSignalsStaysBelowThreshold(t *testing.T) {
	rules := NewRules()
	result := rules.Classify(domain.MessageFeatures{From: "jemand@example.com", FromDomain: "example.com", Subject: "Hallo"}, domain.MailboxProfile{})
	if result.Score >= CandidateThreshold {
		t.Fatalf("neutral message scored %.2f", result.Score)
	}
	if len(result.Evidence) != 0 {
		t.Fatalf("neutral message produced evidence: %+v", result.Evidence)
	}
}
