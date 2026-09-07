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

func TestMachineGeneratedDomainsAreFlagged(t *testing.T) {
	for _, malicious := range []string{
		"stratorechnung8758016.de",
		"barclaysgermany13903.com",
		"dietechnikergermanyd960061.de",
		"amazonsurveygermany51013.com",
		"foodboxlidldeutsch11.de",
	} {
		if !machineGeneratedLabel(malicious) && !digitRun.MatchString(malicious) {
			t.Fatalf("machine-generated domain not flagged: %s", malicious)
		}
	}
}

func TestNormalDomainsAreNotFlagged(t *testing.T) {
	// Pronounceable personal/company domains must never be flagged, even
	// when long. This protects real correspondents from false positives.
	for _, legitimate := range []string{
		"roberthoffmannmedia.de",
		"j-obst.de",
		"griesberger.de",
		"example.com",
		"deutsche-bank.de",
	} {
		if machineGeneratedLabel(legitimate) {
			t.Fatalf("legitimate domain flagged as machine-generated: %s", legitimate)
		}
		if digitRun.MatchString(legitimate) {
			t.Fatalf("legitimate domain flagged for digit run: %s", legitimate)
		}
	}
}

func TestPhishingWaveReachesReviewThreshold(t *testing.T) {
	rules := NewRules()
	msg := domain.MessageFeatures{
		From: "noreply@barclaysgermany13903.com", FromDomain: "barclaysgermany13903.com",
		Subject: "Bestätigen Sie Ihre Identität zur Vermeidung einer Sperre!!",
		Text:    "Bitte bestätigen Sie jetzt Ihre Identität, sonst wird Ihr Konto gesperrt.",
	}
	result := rules.Classify(msg, domain.MailboxProfile{})
	if result.Score < CandidateThreshold {
		t.Fatalf("phishing mail scored %.2f, want >= %.2f", result.Score, CandidateThreshold)
	}
	if result.IndependentGroups < 2 {
		t.Fatalf("phishing mail needs >= 2 independent groups, got %d", result.IndependentGroups)
	}
	if findEvidence(result.Evidence, CodeSenderDigitPattern) == nil || findEvidence(result.Evidence, CodeVerificationRequest) == nil {
		t.Fatalf("expected sender + verification evidence: %+v", result.Evidence)
	}
}

func TestKnownCorrespondentNeverFlaggedDespiteOddDomain(t *testing.T) {
	rules := NewRules()
	msg := domain.MessageFeatures{From: "chef@firma12345.de", FromDomain: "firma12345.de", Subject: "Termin", KnownCorrespondent: true}
	result := rules.Classify(msg, domain.MailboxProfile{})
	if !result.StrongTrustSignal {
		t.Fatal("known correspondent must create a trust signal")
	}
	if Decide(domain.SafetySafe, result) == ActionMove {
		t.Fatal("known correspondent must never be moved automatically")
	}
}
