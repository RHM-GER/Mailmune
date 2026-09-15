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

func TestSubjectEmojiIsWeakIndicator(t *testing.T) {
	rules := NewRules()

	// Emoji alone: produces evidence but stays below the candidate threshold,
	// so legitimate emoji-bearing mail (newsletters, personal) is not flagged.
	plain := rules.Classify(domain.MessageFeatures{From: "freund@example.com", FromDomain: "example.com", Subject: "Hallo 👋"}, domain.MailboxProfile{})
	if findEvidence(plain.Evidence, CodeSubjectEmoji) == nil {
		t.Fatalf("emoji evidence missing: %+v", plain.Evidence)
	}
	if plain.Score >= CandidateThreshold {
		t.Fatalf("emoji alone must stay below threshold, got %.2f", plain.Score)
	}

	// A formal/serious subject that already trips a content signal scores
	// higher with an emoji than without it (the mismatch indicator).
	base := domain.MessageFeatures{From: "service@bank.example", FromDomain: "bank.example"}
	withEmoji := rules.Classify(withSubject(base, "Ihre Zahlung ist fehlgeschlagen ⚠️"), domain.MailboxProfile{})
	withoutEmoji := rules.Classify(withSubject(base, "Ihre Zahlung ist fehlgeschlagen"), domain.MailboxProfile{})
	if findEvidence(withEmoji.Evidence, CodeSubjectEmoji) == nil {
		t.Fatalf("emoji evidence missing on formal subject: %+v", withEmoji.Evidence)
	}
	if withEmoji.Score <= withoutEmoji.Score {
		t.Fatalf("emoji should raise the score: with=%.3f without=%.3f", withEmoji.Score, withoutEmoji.Score)
	}

	// Ordinary text and umlauts must never match the emoji ranges.
	for _, subject := range []string{"Rechnung Nr. 2024-0815", "Grüße aus München", "ÄÖÜ ß – Meeting"} {
		if findEvidence(rules.Classify(withSubject(base, subject), domain.MailboxProfile{}).Evidence, CodeSubjectEmoji) != nil {
			t.Fatalf("false emoji hit for %q", subject)
		}
	}
}

func TestGenericSpamSignalsServerMarkerBlackmailSpoofing(t *testing.T) {
	rules := NewRules()

	// The receiving server tagged the subject as spam.
	marked := rules.Classify(domain.MessageFeatures{From: "x@partner.example", FromDomain: "partner.example", Subject: "*** Spam *** Bitcoin Investment"}, domain.MailboxProfile{})
	if findEvidence(marked.Evidence, CodeServerMarkedSpam) == nil {
		t.Fatalf("server spam marker missing: %+v", marked.Evidence)
	}

	// Sextortion / blackmail threat reaches the review threshold on its own.
	blackmail := rules.Classify(domain.MessageFeatures{From: "x@partner.example", FromDomain: "partner.example", Subject: "Ihr Video wird an Ihre Familie gesendet"}, domain.MailboxProfile{})
	if findEvidence(blackmail.Evidence, CodeBlackmail) == nil {
		t.Fatalf("blackmail signal missing: %+v", blackmail.Evidence)
	}
	if blackmail.Score < CandidateThreshold {
		t.Fatalf("sextortion should reach the review threshold, got %.2f", blackmail.Score)
	}

	// An incoming mail claiming the owner's own domain is flagged as spoofing.
	spoof := rules.Classify(domain.MessageFeatures{From: "info@eigene-domain.de", FromDomain: "eigene-domain.de", OwnDomain: "eigene-domain.de", Subject: "Rechnung"}, domain.MailboxProfile{})
	if findEvidence(spoof.Evidence, CodeSpoofedSender) == nil {
		t.Fatalf("spoofed sender missing: %+v", spoof.Evidence)
	}

	// A known correspondent on the same domain must NOT be flagged as spoofing.
	colleague := rules.Classify(domain.MessageFeatures{From: "kollegin@firma.example", FromDomain: "firma.example", OwnDomain: "firma.example", Subject: "Meeting morgen", KnownCorrespondent: true}, domain.MailboxProfile{})
	if findEvidence(colleague.Evidence, CodeSpoofedSender) != nil {
		t.Fatalf("known correspondent must not be flagged as spoofing: %+v", colleague.Evidence)
	}
}

func TestBrandImpersonationAndReturnPathMismatch(t *testing.T) {
	rules := NewRules()

	// A paypal look-alike domain is flagged and reaches the review threshold;
	// the brand's own domain is not.
	fake := rules.Classify(domain.MessageFeatures{From: "rechnung@rfcrecouvpaypal.com", FromDomain: "rfcrecouvpaypal.com", Subject: "Hinweis auf unbezahlten Betrag!"}, domain.MailboxProfile{})
	if findEvidence(fake.Evidence, CodeBrandImpersonation) == nil {
		t.Fatalf("brand impersonation missing: %+v", fake.Evidence)
	}
	if findEvidence(fake.Evidence, CodeFinancialPressure) == nil {
		t.Fatalf("unbezahlter Betrag must trigger financial pressure: %+v", fake.Evidence)
	}
	if fake.Score < CandidateThreshold {
		t.Fatalf("fake paypal invoice should reach the review threshold: %.2f", fake.Score)
	}
	real := rules.Classify(domain.MessageFeatures{From: "service@paypal.com", FromDomain: "paypal.com", Subject: "Ihre Zahlung"}, domain.MailboxProfile{})
	if findEvidence(real.Evidence, CodeBrandImpersonation) != nil {
		t.Fatalf("the brand's own domain must not be flagged: %+v", real.Evidence)
	}

	// Envelope/From mismatch is a spoofing hint; mailing lists are exempt.
	mismatch := rules.Classify(domain.MessageFeatures{From: "chef@firma.example", FromDomain: "firma.example", ReturnPath: "bounce@bulkmail.example", Subject: "Termin"}, domain.MailboxProfile{})
	if findEvidence(mismatch.Evidence, CodeSenderMismatch) == nil {
		t.Fatalf("return-path mismatch missing: %+v", mismatch.Evidence)
	}
	list := rules.Classify(domain.MessageFeatures{From: "news@shop.example", FromDomain: "shop.example", ReturnPath: "bounce@mailer.example", ListUnsubscribe: true, Subject: "Angebote"}, domain.MailboxProfile{})
	if findEvidence(list.Evidence, CodeSenderMismatch) != nil {
		t.Fatalf("mailing list must not be flagged for envelope mismatch: %+v", list.Evidence)
	}

	// Trailing junk must not defeat domain extraction (dash spoofing).
	if got := ExtractDomain("info@eigene-domain.de-"); got != "eigene-domain.de" {
		t.Fatalf("ExtractDomain did not normalize trailing junk: %q", got)
	}
}

func TestAlignedBrandWithAuthIsTrustedNotMismatched(t *testing.T) {
	rules := NewRules()

	// Genuine brand mail: canonical domain, aligned envelope (subdomain),
	// authentication passed. Must be trusted and stay out of the review list.
	genuine := rules.Classify(domain.MessageFeatures{
		From: "businessprofile-noreply@google.com", FromDomain: "google.com",
		ReturnPath: "bounce@accounts.google.com", AuthenticationPassed: true,
		Subject: "Code zur Bestätigung des Unternehmens",
	}, domain.MailboxProfile{})
	if findEvidence(genuine.Evidence, CodeBrandAligned) == nil {
		t.Fatalf("brand_aligned evidence missing: %+v", genuine.Evidence)
	}
	if !genuine.StrongTrustSignal {
		t.Fatal("aligned authenticated brand mail must create a strong trust signal")
	}
	if findEvidence(genuine.Evidence, CodeSenderMismatch) != nil {
		t.Fatalf("subdomain envelope must not trigger sender_mismatch: %+v", genuine.Evidence)
	}
	if genuine.Score >= CandidateThreshold {
		t.Fatalf("genuine brand mail must stay below review threshold, got %.2f", genuine.Score)
	}

	// Brand siblings count as aligned (google.com vs googlemail.com).
	sibling := rules.Classify(domain.MessageFeatures{
		From: "team@google.com", FromDomain: "google.com",
		ReturnPath: "no-reply@googlemail.com", AuthenticationPassed: true,
		Subject: "Ihre Anfrage",
	}, domain.MailboxProfile{})
	if findEvidence(sibling.Evidence, CodeSenderMismatch) != nil {
		t.Fatalf("brand sibling envelope must not trigger sender_mismatch: %+v", sibling.Evidence)
	}
	if findEvidence(sibling.Evidence, CodeBrandAligned) == nil {
		t.Fatalf("brand sibling alignment missing: %+v", sibling.Evidence)
	}

	// Spoofed brand: From claims google.com but the envelope does not align.
	// The mismatch fires and no trust is granted.
	spoofed := rules.Classify(domain.MessageFeatures{
		From: "service@google.com", FromDomain: "google.com",
		ReturnPath: "bounce@evil.example", AuthenticationPassed: true,
		Subject: "Konto bestätigen",
	}, domain.MailboxProfile{})
	if findEvidence(spoofed.Evidence, CodeSenderMismatch) == nil {
		t.Fatalf("envelope mismatch must fire for spoofed brand mail: %+v", spoofed.Evidence)
	}
	if findEvidence(spoofed.Evidence, CodeBrandAligned) != nil {
		t.Fatalf("misaligned envelope must not grant brand trust: %+v", spoofed.Evidence)
	}
	if spoofed.StrongTrustSignal {
		t.Fatal("spoofed brand mail must not create a strong trust signal")
	}

	// Without passed authentication no brand trust is granted, even when the
	// envelope aligns: SPF/DKIM are what make the envelope believable.
	unauthenticated := rules.Classify(domain.MessageFeatures{
		From: "team@google.com", FromDomain: "google.com",
		ReturnPath: "bounce@accounts.google.com",
		Subject:      "Ihre Anfrage",
	}, domain.MailboxProfile{})
	if findEvidence(unauthenticated.Evidence, CodeBrandAligned) != nil {
		t.Fatalf("unauthenticated mail must not gain brand trust: %+v", unauthenticated.Evidence)
	}
}

func TestMachineGeneratedDomainAloneReachesThreshold(t *testing.T) {
	rules := NewRules()
	result := rules.Classify(domain.MessageFeatures{
		From: "postfach@tvprodukt55diehohle.de", FromDomain: "tvprodukt55diehohle.de",
		Subject: "Tschüss Bauchfett! Willkommen Wohlgefühl!",
	}, domain.MailboxProfile{})
	if findEvidence(result.Evidence, CodeMachineGenerated) == nil {
		t.Fatalf("machine_generated evidence missing: %+v", result.Evidence)
	}
	if result.Score < CandidateThreshold {
		t.Fatalf("machine-generated sender domain must reach the review threshold on its own, got %.2f", result.Score)
	}
}

func withSubject(msg domain.MessageFeatures, subject string) domain.MessageFeatures {
	msg.Subject = subject
	return msg
}
