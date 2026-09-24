package classifier

import (
	"testing"

	"github.com/RHM-GER/Mailmune/internal/domain"
)

// designProfile ist das Beispielprofil eines Mediengestalters: erwartet wird
// Kundenkorrespondenz zu Design/Video – keine Diät-Pillen, keine Krypto-Anlagen.
var designProfile = domain.MailboxProfile{
	Purpose:           "Posteingang einer Design-Agentur: Mediengestaltung, Webdesign und Videoproduktion für Unternehmenskunden",
	Industry:          "Design und Medien",
	ExpectedMailTypes: []string{"Kundenanfragen", "Angebote und Rechnungen"},
}

func findCode(evidence []domain.Evidence, code string) *domain.Evidence {
	for i := range evidence {
		if evidence[i].Code == code {
			return &evidence[i]
		}
	}
	return nil
}

// TestDietVerticalWithProfileMismatchLiftsObviousSpam: Die Diät-Kampagne
// (echter Fall: „Tschüss Bauchfett! Willkommen Wohlgefühl!“ von einer
// maschinell erzeugten Domain) muss klar über der Review-Schwelle landen –
// über GENERISCHE Signale (Vertikale + Profilabweichung), nicht über
// hartkodierte Absender.
func TestDietVerticalWithProfileMismatchLiftsObviousSpam(t *testing.T) {
	rules := NewRules()
	msg := domain.MessageFeatures{
		From: "Tschüss Bauchfett <tv@tvprodukt55diehohle.de>", FromDomain: "tvprodukt55diehohle.de",
		Subject: "Tschüss Bauchfett! Willkommen Wohlgefühl!",
		Text:    "Ernährungsmediziner empfehlen diese Formel. Schnell abnehmen ohne Diät-Spray oder Keto-Gummies. Ihr Gesundheitsvorteil wartet.",
	}
	classification := rules.Classify(msg, designProfile)
	if findCode(classification.Evidence, CodeSpamVertical) == nil {
		t.Fatalf("spam_vertical evidence missing: %+v", classification.Evidence)
	}
	if findCode(classification.Evidence, CodeProfileMismatch) == nil {
		t.Fatalf("profile_mismatch evidence missing: %+v", classification.Evidence)
	}
	if classification.Score < 0.85 {
		t.Fatalf("score = %.2f, want >= 0.85 (eindeutige Kampagne + fremdes Profil)", classification.Score)
	}
}

// TestInsurerBaitVertical: Krankenkassen-/Medicare-Lockangebote müssen als
// Kampagnenkategorie erkannt werden (Fake-Kassen-Domains zusätzlich über
// machine_generated_domain).
func TestInsurerBaitVertical(t *testing.T) {
	rules := NewRules()
	msg := domain.MessageFeatures{
		From: "Service <kontakdkt@tkservdice.de>", FromDomain: "tkservdice.de",
		Subject: "Kostenlos testen: Das neue Medicare Kit für Sie bereit!",
		Text:    "Ihre Auswahl wurde bestätigt. Das Medicare Kit ist bereit zum Versand.",
	}
	classification := rules.Classify(msg, designProfile)
	if findCode(classification.Evidence, CodeSpamVertical) == nil {
		t.Fatalf("campaign vertical missing: %+v", classification.Evidence)
	}
	if classification.Score < 0.70 {
		t.Fatalf("score = %.2f, want >= 0.70", classification.Score)
	}
}

// TestWalletKycVertical: KYC-/Wallet-Phishing landet über die Vertikale im
// Review-Bereich.
func TestWalletKycVertical(t *testing.T) {
	rules := NewRules()
	msg := domain.MessageFeatures{
		From: "Phantom <noreply@phantomwallet-kycgermany.com>", FromDomain: "phantomwallet-kycgermany.com",
		Subject: "Wichtige KYC-Anforderung - Wallet-Verifizierung",
		Text:    "Bitte verifizieren Sie Ihre Wallet innerhalb von 24 Stunden.",
	}
	classification := rules.Classify(msg, designProfile)
	if findCode(classification.Evidence, CodeSpamVertical) == nil {
		t.Fatalf("kyc vertical missing: %+v", classification.Evidence)
	}
	if classification.Score < 0.70 {
		t.Fatalf("score = %.2f, want >= 0.70", classification.Score)
	}
}

// TestPotencyVerticalScam: Die Sex-/Potenz-Masche („mach sie völlig
// sprachlos“) ist eine eigene Kampagnenkategorie mit hohem Gewicht.
func TestPotencyVerticalScam(t *testing.T) {
	rules := NewRules()
	msg := domain.MessageFeatures{
		From: "info@atmosphere.fnq.ker.mybluehost.me", FromDomain: "atmosphere.fnq.ker.mybluehost.me",
		Subject: "roberthoffmannmedia, mach sie völlig sprachlos ➔",
		Text:    "Wollen Sie das Knistern und die pure Lust wieder voll auskosten? Spontaner, ausdauernder und zutiefst befriedigender Sex ist jetzt wieder jederzeit für Sie möglich.",
	}
	classification := rules.Classify(msg, designProfile)
	if findCode(classification.Evidence, CodeSpamVertical) == nil {
		t.Fatalf("potency vertical missing: %+v", classification.Evidence)
	}
	if classification.Score < 0.70 {
		t.Fatalf("score = %.2f, want >= 0.70", classification.Score)
	}
}

// TestProfileMatchSuppressesMismatchEvidence: Eine Diät-Mail an ein
// Ernährungsberatungs-Profil bekommt KEIN profile_mismatch – die Vertikale
// allein bleibt unter der Schwelle, damit thematisch passende Post nicht
// vorverurteilt wird.
func TestProfileMatchSuppressesMismatchEvidence(t *testing.T) {
	rules := NewRules()
	nutritionProfile := domain.MailboxProfile{
		Purpose:  "Praxis für Ernährungsberatung: Beratung zu Ernährung, Diät und Abnehmen",
		Industry: "Gesundheit und Ernährung",
	}
	msg := domain.MessageFeatures{
		From: "newsletter@ernaehrung-fachverlag.de", FromDomain: "ernaehrung-fachverlag.de",
		Subject: "Neue Studien zum Thema Abnehmen",
		Text:    "Fachartikel: wissenschaftlich begleitete Diät-Programme für Ihre Beratungspraxis.",
	}
	classification := rules.Classify(msg, nutritionProfile)
	if findCode(classification.Evidence, CodeProfileMismatch) != nil {
		t.Fatalf("profile_mismatch must not fire for on-topic profile: %+v", classification.Evidence)
	}
}

// TestNoVerticalOnNormalBusinessMail: Normale Geschäfts- und Privatpost darf
// niemals in eine Kampagnenkategorie rutschen.
func TestNoVerticalOnNormalBusinessMail(t *testing.T) {
	rules := NewRules()
	msg := domain.MessageFeatures{
		From: "kunde@mittelstand-gmbh.de", FromDomain: "mittelstand-gmbh.de",
		Subject: "Rechnung 2026-114 Designleistung",
		Text:    "Anbei die freigegebene Rechnung für das Logo und die Webseite. Vielen Dank für die Zusammenarbeit.",
	}
	classification := rules.Classify(msg, designProfile)
	if findCode(classification.Evidence, CodeSpamVertical) != nil {
		t.Fatalf("normal business mail must not hit a vertical: %+v", classification.Evidence)
	}
	if classification.Score >= 0.40 {
		t.Fatalf("score = %.2f, want < 0.40 for clean business mail", classification.Score)
	}
}

// TestThinProfileNeverJudgesOffTopic: Ohne aussagekräftiges Profil (weniger
// als drei Vokabeln) gibt es kein Off-Topic-Urteil – sonst würde ein leeres
// Profil jede Kampagnen-Mail künstlich verstärken.
func TestThinProfileNeverJudgesOffTopic(t *testing.T) {
	rules := NewRules()
	msg := domain.MessageFeatures{
		From: "tv@tvprodukt55diehohle.de", FromDomain: "tvprodukt55diehohle.de",
		Subject: "Tschüss Bauchfett!",
		Text:    "Schnell abnehmen.",
	}
	classification := rules.Classify(msg, domain.MailboxProfile{Purpose: "Firma"})
	if findCode(classification.Evidence, CodeProfileMismatch) != nil {
		t.Fatalf("thin profile must not produce profile_mismatch: %+v", classification.Evidence)
	}
}
