package classifier

import (
	"math"
	"net/mail"
	"regexp"
	"strings"

	"github.com/RHM-GER/Mailmune/internal/domain"
)

var (
	urgentTerms = regexp.MustCompile(`(?i)\b(sofort handeln|konto gesperrt|gewinn|lotterie|krypto.?investment|passwort bestätigen|zahlung fehlgeschlagen)\b`)
	trackingURL = regexp.MustCompile(`(?i)(bit\.ly|tinyurl\.com|t\.co|cutt\.ly|rb\.gy)/`)
)

type Rules struct{}

func NewRules() *Rules { return &Rules{} }

func (r *Rules) Classify(msg domain.MessageFeatures, profile domain.MailboxProfile) domain.Classification {
	evidence := make([]domain.Evidence, 0, 8)
	strongTrust := msg.KnownCorrespondent || containsFold(profile.TrustedSenders, msg.From) || containsFold(profile.TrustedDomains, msg.FromDomain)

	if strongTrust {
		evidence = append(evidence, domain.Evidence{Group: "relationship", Code: "known_correspondent", Weight: -0.9, Summary: "Bekannter oder ausdrücklich vertrauter Korrespondenzpartner"})
	}
	if msg.AuthenticationPassed {
		evidence = append(evidence, domain.Evidence{Group: "authentication", Code: "auth_pass", Weight: -0.25, Summary: "Vertrauenswürdige Mailserver-Prüfung bestanden"})
	}
	if msg.AuthenticationFailed {
		evidence = append(evidence, domain.Evidence{Group: "authentication", Code: "auth_fail", Weight: 0.55, Summary: "Mailserver meldet fehlgeschlagene Absenderprüfung"})
	}
	if urgentTerms.MatchString(msg.Subject + " " + msg.Text) {
		evidence = append(evidence, domain.Evidence{Group: "content", Code: "pressure_language", Weight: 0.35, Summary: "Typische Druck- oder Lockformulierungen erkannt"})
	}
	if trackingURL.MatchString(msg.Text) || msg.URLCount >= 8 {
		evidence = append(evidence, domain.Evidence{Group: "links", Code: "suspicious_links", Weight: 0.35, Summary: "Ungewöhnlich viele oder verkürzte Links"})
	}
	if msg.ListUnsubscribe {
		evidence = append(evidence, domain.Evidence{Group: "mailing_list", Code: "list_unsubscribe", Weight: -0.08, Summary: "Reguläre Mailinglisten-Kopfzeile vorhanden"})
	}
	for _, attachment := range msg.Attachments {
		name := strings.ToLower(attachment.Filename)
		if strings.HasSuffix(name, ".exe") || strings.HasSuffix(name, ".js") || strings.HasSuffix(name, ".scr") || strings.HasSuffix(name, ".iso") {
			evidence = append(evidence, domain.Evidence{Group: "attachment", Code: "dangerous_attachment_type", Weight: 0.5, Summary: "Riskanter Dateityp im Anhang"})
			break
		}
	}

	// A bounded logistic transform keeps independently explainable weights while
	// preventing one weak heuristic from becoming an automatic action.
	logit := -1.25
	groups := map[string]struct{}{}
	for _, item := range evidence {
		logit += item.Weight * 3
		if item.Weight > 0 {
			groups[item.Group] = struct{}{}
		}
	}
	score := 1 / (1 + math.Exp(-logit))
	if strongTrust && score > 0.79 {
		score = 0.79
	}

	return domain.Classification{
		Score:             clamp(score),
		Evidence:          evidence,
		IndependentGroups: len(groups),
		StrongTrustSignal: strongTrust,
	}
}

func ExtractDomain(address string) string {
	parsed, err := mail.ParseAddress(address)
	if err == nil {
		address = parsed.Address
	}
	parts := strings.Split(strings.ToLower(strings.TrimSpace(address)), "@")
	if len(parts) != 2 {
		return ""
	}
	return strings.TrimSuffix(parts[1], ".")
}

func containsFold(items []string, value string) bool {
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item), strings.TrimSpace(value)) {
			return true
		}
	}
	return false
}

func clamp(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
