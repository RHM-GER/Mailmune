package classifier

import (
	"math"
	"net/mail"
	"regexp"
	"strings"

	"github.com/RHM-GER/Mailmune/internal/domain"
)

// Stable evidence codes. Every code is documented in docs/evidence-codes.md
// and must not change meaning without a contract version bump.
const (
	CodeAllowSender          = "allow_sender"
	CodeAllowDomain          = "allow_domain"
	CodeDenySender           = "deny_sender"
	CodeDenyDomain           = "deny_domain"
	CodeDenyKeyword          = "deny_keyword"
	CodeKnownCorrespondent   = "known_correspondent"
	CodeAuthPass             = "auth_pass"
	CodeAuthFail             = "auth_fail"
	CodePressureLanguage     = "pressure_language"
	CodeSuspiciousLinks      = "suspicious_links"
	CodeURLShortener         = "url_shortener"
	CodeListUnsubscribe      = "list_unsubscribe"
	CodeDangerousAttachment  = "dangerous_attachment_type"
	CodeStatisticalSpam      = "statistical_spam"
	CodeStatisticalHam       = "statistical_ham"
	CodeLocalModelPrefix     = "local_model_"
)

var (
	urgentTerms = regexp.MustCompile(`(?i)\b(sofort handeln|konto gesperrt|gewinn|lotterie|krypto.?investment|passwort bestätigen|zahlung fehlgeschlagen)\b`)
	trackingURL = regexp.MustCompile(`(?i)(bit\.ly|tinyurl\.com|t\.co|cutt\.ly|rb\.gy)/`)
)

// StatisticalScorer evaluates learned token features. The local learner
// implements it; it is optional and may be nil.
type StatisticalScorer interface {
	Score(features map[string]int) (float64, bool)
}

// Rules evaluates the deterministic stage of the classification pipeline.
// Each stage contributes independently explainable evidence with a stable
// code; the combined score stays conservative unless several independent
// signal groups agree.
type Rules struct{}

func NewRules() *Rules { return &Rules{} }

// Classify evaluates the deterministic rules without a statistical model.
func (r *Rules) Classify(msg domain.MessageFeatures, profile domain.MailboxProfile) domain.Classification {
	return r.ClassifyWithFeatures(msg, profile, nil, nil)
}

// ClassifyWithFeatures additionally folds the local statistical learner into
// the result when a scorer and its feature vector are provided.
func (r *Rules) ClassifyWithFeatures(msg domain.MessageFeatures, profile domain.MailboxProfile, features map[string]int, scorer StatisticalScorer) domain.Classification {
	evidence := make([]domain.Evidence, 0, 8)

	strongTrust := r.stageTrust(msg, profile, &evidence)
	r.stageDeny(msg, profile, &evidence)
	r.stageAuthentication(msg, &evidence)
	r.stageContent(msg, &evidence)
	r.stageLinks(msg, &evidence)
	r.stageMailingList(msg, &evidence)
	r.stageAttachments(msg, &evidence)
	r.stageStatistical(features, scorer, &evidence)

	// A bounded logistic transform keeps independently explainable weights
	// while preventing one weak heuristic from becoming an automatic action.
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

// stageTrust collects explicit allow-list and relationship signals. They
// never trigger moves themselves but cap the score via StrongTrustSignal.
func (r *Rules) stageTrust(msg domain.MessageFeatures, profile domain.MailboxProfile, evidence *[]domain.Evidence) bool {
	strongTrust := false
	if msg.KnownCorrespondent {
		strongTrust = true
		*evidence = append(*evidence, domain.Evidence{Group: "relationship", Code: CodeKnownCorrespondent, Weight: -0.9, Summary: "Bekannter Korrespondenzpartner aus dem eigenen Postausgang"})
	}
	if containsFold(profile.TrustedSenders, msg.From) {
		strongTrust = true
		*evidence = append(*evidence, domain.Evidence{Group: "relationship", Code: CodeAllowSender, Weight: -0.9, Summary: "Absender steht auf der Vertrauensliste"})
	}
	if msg.FromDomain != "" && containsFold(profile.TrustedDomains, msg.FromDomain) {
		strongTrust = true
		*evidence = append(*evidence, domain.Evidence{Group: "relationship", Code: CodeAllowDomain, Weight: -0.8, Summary: "Domain steht auf der Vertrauensliste"})
	}
	return strongTrust
}

// stageDeny applies explicit deny rules. They are strong spam hints but
// still need a second independent signal group for automatic moves.
func (r *Rules) stageDeny(msg domain.MessageFeatures, profile domain.MailboxProfile, evidence *[]domain.Evidence) {
	if containsFold(profile.DeniedSenders, msg.From) {
		*evidence = append(*evidence, domain.Evidence{Group: "rules", Code: CodeDenySender, Weight: 0.85, Summary: "Absender steht auf der Sperrliste"})
	}
	if msg.FromDomain != "" && containsFold(profile.DeniedDomains, msg.FromDomain) {
		*evidence = append(*evidence, domain.Evidence{Group: "rules", Code: CodeDenyDomain, Weight: 0.75, Summary: "Domain steht auf der Sperrliste"})
	}
	for _, keyword := range profile.DeniedKeywords {
		keyword = strings.TrimSpace(keyword)
		if keyword == "" {
			continue
		}
		if strings.Contains(strings.ToLower(msg.Subject), strings.ToLower(keyword)) || strings.Contains(strings.ToLower(msg.Text), strings.ToLower(keyword)) {
			*evidence = append(*evidence, domain.Evidence{Group: "rules", Code: CodeDenyKeyword, Weight: 0.5, Summary: "Gesperrtes Schlüsselwort erkannt"})
			break
		}
	}
}

func (r *Rules) stageAuthentication(msg domain.MessageFeatures, evidence *[]domain.Evidence) {
	if msg.AuthenticationPassed {
		*evidence = append(*evidence, domain.Evidence{Group: "authentication", Code: CodeAuthPass, Weight: -0.25, Summary: "Vertrauenswürdige Mailserver-Prüfung bestanden"})
	}
	if msg.AuthenticationFailed {
		*evidence = append(*evidence, domain.Evidence{Group: "authentication", Code: CodeAuthFail, Weight: 0.55, Summary: "Mailserver meldet fehlgeschlagene Absenderprüfung"})
	}
}

func (r *Rules) stageContent(msg domain.MessageFeatures, evidence *[]domain.Evidence) {
	if urgentTerms.MatchString(msg.Subject + " " + msg.Text) {
		*evidence = append(*evidence, domain.Evidence{Group: "content", Code: CodePressureLanguage, Weight: 0.35, Summary: "Typische Druck- oder Lockformulierungen erkannt"})
	}
}

func (r *Rules) stageLinks(msg domain.MessageFeatures, evidence *[]domain.Evidence) {
	if trackingURL.MatchString(msg.Text) {
		*evidence = append(*evidence, domain.Evidence{Group: "links", Code: CodeURLShortener, Weight: 0.35, Summary: "Verkürzte Links erkannt"})
	} else if msg.URLCount >= 8 {
		*evidence = append(*evidence, domain.Evidence{Group: "links", Code: CodeSuspiciousLinks, Weight: 0.35, Summary: "Ungewöhnlich viele Links"})
	}
}

func (r *Rules) stageMailingList(msg domain.MessageFeatures, evidence *[]domain.Evidence) {
	if msg.ListUnsubscribe {
		*evidence = append(*evidence, domain.Evidence{Group: "mailing_list", Code: CodeListUnsubscribe, Weight: -0.08, Summary: "Reguläre Mailinglisten-Kopfzeile vorhanden"})
	}
}

func (r *Rules) stageAttachments(msg domain.MessageFeatures, evidence *[]domain.Evidence) {
	for _, attachment := range msg.Attachments {
		name := strings.ToLower(attachment.Filename)
		if strings.HasSuffix(name, ".exe") || strings.HasSuffix(name, ".js") || strings.HasSuffix(name, ".scr") || strings.HasSuffix(name, ".iso") {
			*evidence = append(*evidence, domain.Evidence{Group: "attachment", Code: CodeDangerousAttachment, Weight: 0.5, Summary: "Riskanter Dateityp im Anhang"})
			break
		}
	}
}

// stageStatistical folds the local learner in as exactly one independent
// signal group. An untrained or uncertain model contributes nothing.
func (r *Rules) stageStatistical(features map[string]int, scorer StatisticalScorer, evidence *[]domain.Evidence) {
	if scorer == nil || len(features) == 0 {
		return
	}
	probability, ok := scorer.Score(features)
	if !ok {
		return
	}
	switch {
	case probability >= 0.75:
		weight := clampRange((probability-0.5)*1.6, 0, 0.8)
		*evidence = append(*evidence, domain.Evidence{Group: "statistical", Code: CodeStatisticalSpam, Weight: weight, Summary: "Lokaler Lernfilter erkennt ein Spam-Muster"})
	case probability <= 0.25:
		weight := -clampRange((0.5-probability)*1.6, 0, 0.8)
		*evidence = append(*evidence, domain.Evidence{Group: "statistical", Code: CodeStatisticalHam, Weight: weight, Summary: "Lokaler Lernfilter erkennt ein vertrautes Muster"})
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
	if value == "" {
		return false
	}
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

func clampRange(value, minimum, maximum float64) float64 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}
