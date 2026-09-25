package classifier

import (
	"testing"

	"github.com/RHM-GER/Mailmune/internal/domain"
)

// TestOwnWebsiteMailIsTrustedNotSpoofed: WordPress-Formularbenachrichtigung von
// der eigenen Domain mit passender Envelope ist Infrastruktur, kein Spoofing.
func TestOwnWebsiteMailIsTrustedNotSpoofed(t *testing.T) {
	rules := NewRules()
	msg := domain.MessageFeatures{
		From: "wordpress@meineseite.de", FromDomain: "meineseite.de",
		OwnDomain:  "meineseite.de",
		ReturnPath: "wordpress@meineseite.de",
		Subject:    "Neue Formular-Nachricht: Kontaktformular",
		Text:       "Ihr Formular wurde abgesendet. Name: Kundin, Nachricht: Bitte um Rueckruf.",
	}
	c := rules.Classify(msg, domain.MailboxProfile{Purpose: "Anfragen ueber die eigene Website"})
	if findCode(c.Evidence, CodeSpoofedSender) != nil {
		t.Fatalf("eigene Website-Mail darf nicht als Spoofing gelten: %+v", c.Evidence)
	}
	if findCode(c.Evidence, CodeOwnDomainAligned) == nil {
		t.Fatalf("own_domain_aligned Evidence fehlt: %+v", c.Evidence)
	}
	if c.Score >= 0.25 {
		t.Fatalf("score = %.2f, want < 0.25 für eigene Infrastruktur-Mail", c.Score)
	}
}

// TestSextortionOwnDomainStillFlagged: gefälschte eigene Adresse mit FREMDER
// Envelope bleibt Spoofing – der Kernfall der Regel.
func TestSextortionOwnDomainStillFlagged(t *testing.T) {
	rules := NewRules()
	msg := domain.MessageFeatures{
		From: "info@meineseite.de", FromDomain: "meineseite.de",
		OwnDomain:  "meineseite.de",
		ReturnPath: "random@foreign-host.example",
		Subject:    "Sehr wichtige Mitteilung zu Ihrem Video!",
	}
	c := rules.Classify(msg, domain.MailboxProfile{})
	if findCode(c.Evidence, CodeSpoofedSender) == nil {
		t.Fatalf("fremde Envelope bei eigener From muss Spoofing bleiben: %+v", c.Evidence)
	}
	if findCode(c.Evidence, CodeOwnDomainAligned) != nil {
		t.Fatal("own_domain_aligned darf bei fremder Envelope nicht feuern")
	}
}

// TestOwnDomainMissingEnvelopeStillFlagged: fehlende Envelope ist kein
// Intern-Merkmal (Sextortion liefert das häufig).
func TestOwnDomainMissingEnvelopeStillFlagged(t *testing.T) {
	rules := NewRules()
	msg := domain.MessageFeatures{
		From: "info@meineseite.de", FromDomain: "meineseite.de",
		OwnDomain: "meineseite.de",
		Subject:   "Ich habe dich gefilmt",
	}
	c := rules.Classify(msg, domain.MailboxProfile{})
	if findCode(c.Evidence, CodeSpoofedSender) == nil {
		t.Fatalf("fehlende Envelope bei eigener From muss Spoofing bleiben: %+v", c.Evidence)
	}
}

// TestOwnDomainWithFailedAuthStillFlagged: Auth-Fehlschlag + eigene Domain =
// kein Vertrauen, Spoofing-Signal bleibt.
func TestOwnDomainWithFailedAuthStillFlagged(t *testing.T) {
	rules := NewRules()
	msg := domain.MessageFeatures{
		From: "wordpress@meineseite.de", FromDomain: "meineseite.de",
		OwnDomain:          "meineseite.de",
		ReturnPath:         "wordpress@meineseite.de",
		AuthenticationFailed: true,
		Subject:            "Neue Formular-Nachricht",
	}
	c := rules.Classify(msg, domain.MailboxProfile{})
	if findCode(c.Evidence, CodeOwnDomainAligned) != nil {
		t.Fatal("Auth-Fehlschlag muss own-domain-Trust blockieren")
	}
	if findCode(c.Evidence, CodeSpoofedSender) == nil {
		t.Fatal("Auth-Fehlschlag bei eigener Domain muss Spoofing-Signal behalten")
	}
}
