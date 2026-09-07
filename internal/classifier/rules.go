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
	CodeRewardBait           = "reward_bait"
	CodeVerificationRequest  = "verification_request"
	CodeFinancialPressure    = "financial_pressure"
	CodeSubjectAnomaly       = "subject_anomaly"
	CodeSuspiciousLinks      = "suspicious_links"
	CodeURLShortener         = "url_shortener"
	CodeListUnsubscribe      = "list_unsubscribe"
	CodeDangerousAttachment  = "dangerous_attachment_type"
	CodeStatisticalSpam      = "statistical_spam"
	CodeStatisticalHam       = "statistical_ham"
	CodeMachineGenerated     = "machine_generated_domain"
	CodeSenderDigitPattern   = "sender_digit_pattern"
	CodeLocalModelPrefix     = "local_model_"
)

var (
	// Generic pressure/threat language (German + English).
	urgencyTerms = regexp.MustCompile(`(?i)(sofort handeln|dringend|konto gesperrt|gesperrt|sperrung|letzte warnung|letztmalig|läuft ab|frist (läuft|endet)|zahlung fehlgeschlagen|fehlgeschlagene zahlung|ungewöhnliche aktivität|act now|urgent|immediately|suspended|final notice|last warning)`)
	// Reward/bait language.
	rewardTerms = regexp.MustCompile(`(?i)(gewinn|gewonnen|lotterie|glücklich|glücks|gratis|kostenlos|geschenk|überraschung wartet|bonus|prämie|exklusiv (für|nur)|sie gehören zu den|jackpot|cashback|erstattung|gutschein wartet)`)
	// Requests to confirm identity or credentials.
	verifyTerms = regexp.MustCompile(`(?i)(bestätigen sie|bitte bestätigen|identität bestätigen|identität verifizieren|verifizieren sie|konto aktualisieren|daten aktualisieren|angaben aktualisieren|passwort bestätigen|klicken sie (hier|unten)|jetzt anmelden und|confirm (your|now)|verify (your|now)|update (your|now))`)
	// Financial pressure patterns.
	financialTerms = regexp.MustCompile(`(?i)(steht noch aus|ausstehende zahlung|bitte.{0,20}begleichen|offene (rechnung|forderung)|mahnung|inkasso|zahlungserinnerung|überprüfen sie ihr konto|kontoaktualisierung erforderlich)`)
	trackingURL    = regexp.MustCompile(`(?i)(bit\.ly|tinyurl\.com|t\.co|cutt\.ly|rb\.gy)/`)
	digitRun       = regexp.MustCompile(`[0-9]{4,}`)
	digitAnywhere  = regexp.MustCompile(`[0-9]`)
	doubleBang     = regexp.MustCompile(`!{2,}`)
	uppercaseWord  = regexp.MustCompile(`\b[A-ZÄÖÜ]{6,}\b`)
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
	r.stageSenderIntegrity(msg, &evidence)
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

// stageSenderIntegrity inspects the sender domain with generic, data-free
// heuristics: long digit runs and machine-generated (unpronounceable,
// over-long, digit-mixed) labels. No brand lists or external data are used.
func (r *Rules) stageSenderIntegrity(msg domain.MessageFeatures, evidence *[]domain.Evidence) {
	senderDomain := strings.ToLower(strings.TrimSpace(msg.FromDomain))
	if senderDomain == "" {
		return
	}
	if digitRun.MatchString(senderDomain) {
		*evidence = append(*evidence, domain.Evidence{Group: "sender_integrity", Code: CodeSenderDigitPattern, Weight: 0.6, Summary: "Maschinell erzeugte Absenderdomain mit langen Ziffernfolgen"})
	}
	if machineGeneratedLabel(senderDomain) {
		*evidence = append(*evidence, domain.Evidence{Group: "sender_integrity", Code: CodeMachineGenerated, Weight: 0.45, Summary: "Absenderdomain wirkt automatisch zusammengesetzt (unübliche Länge/Ziffern/Vokalanteil)"})
	}
}

// machineGeneratedLabel reports whether a domain label looks concatenated or
// randomly generated. Pronounceable domains of normal length (including long
// personal or company names) are not flagged.
func machineGeneratedLabel(senderDomain string) bool {
	labels := strings.Split(strings.TrimSuffix(senderDomain, "."), ".")
	// Ignore the TLD; analyze the remaining labels.
	for _, label := range labels[:max(len(labels)-1, 0)] {
		if len(label) < 12 {
			continue
		}
		letters := 0
		vowels := 0
		for _, character := range label {
			if character >= 'a' && character <= 'z' {
				letters++
				switch character {
				case 'a', 'e', 'i', 'o', 'u':
					vowels++
				}
			}
		}
		if letters == 0 {
			continue
		}
		vowelRatio := float64(vowels) / float64(letters)
		hasDigit := digitAnywhere.MatchString(label)
		if len(label) >= 16 && (hasDigit || vowelRatio < 0.28) {
			return true
		}
		if vowelRatio < 0.2 && len(label) >= 12 {
			return true
		}
	}
	return false
}

func (r *Rules) stageAuthentication(msg domain.MessageFeatures, evidence *[]domain.Evidence) {
	if msg.AuthenticationPassed {
		*evidence = append(*evidence, domain.Evidence{Group: "authentication", Code: CodeAuthPass, Weight: -0.25, Summary: "Vertrauenswürdige Mailserver-Prüfung bestanden"})
	}
	if msg.AuthenticationFailed {
		*evidence = append(*evidence, domain.Evidence{Group: "authentication", Code: CodeAuthFail, Weight: 0.55, Summary: "Mailserver meldet fehlgeschlagene Absenderprüfung"})
	}
}

// stageContent analyzes subject and bounded text with generic signal
// categories. Each category contributes at most once, so several
// independent wording patterns must coincide for a high score.
func (r *Rules) stageContent(msg domain.MessageFeatures, evidence *[]domain.Evidence) {
	subject := msg.Subject
	combined := msg.Subject + " " + msg.Text
	if urgencyTerms.MatchString(combined) {
		*evidence = append(*evidence, domain.Evidence{Group: "content", Code: CodePressureLanguage, Weight: 0.35, Summary: "Druck- oder Drohformulierung erkannt"})
	}
	if rewardTerms.MatchString(combined) {
		*evidence = append(*evidence, domain.Evidence{Group: "content", Code: CodeRewardBait, Weight: 0.3, Summary: "Lockangebot (Gewinn, Geschenk, Bonus) erkannt"})
	}
	if verifyTerms.MatchString(combined) {
		*evidence = append(*evidence, domain.Evidence{Group: "content", Code: CodeVerificationRequest, Weight: 0.3, Summary: "Aufforderung zur Bestätigung von Identität oder Zugangsdaten"})
	}
	if financialTerms.MatchString(combined) {
		*evidence = append(*evidence, domain.Evidence{Group: "content", Code: CodeFinancialPressure, Weight: 0.25, Summary: "Finanzielle Druckformulierung (offene Zahlung, Mahnung, Konto)"})
	}
	if doubleBang.MatchString(subject) || uppercaseWord.MatchString(subject) {
		*evidence = append(*evidence, domain.Evidence{Group: "content", Code: CodeSubjectAnomaly, Weight: 0.15, Summary: "Auffällige Zeichensetzung oder Schreibung im Betreff"})
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

// CountURLs counts URL occurrences in bounded text offline. It never
// resolves or fetches anything; the count is a transient heuristic only.
func CountURLs(text string) int {
	lower := strings.ToLower(text)
	if len(lower) > 64<<10 {
		lower = lower[:64<<10]
	}
	return strings.Count(lower, "http://") + strings.Count(lower, "https://")
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
