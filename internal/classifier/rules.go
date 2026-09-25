package classifier

import (
	"math"
	"net/mail"
	"regexp"
	"strings"
	"unicode"

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
	CodeSubjectEmoji         = "subject_emoji"
	CodeServerMarkedSpam     = "server_marked_spam"
	CodeBlackmail            = "blackmail_threat"
	CodeSpoofedSender        = "spoofed_sender"
	CodeSenderMismatch       = "sender_mismatch"
	CodeBrandImpersonation   = "brand_impersonation"
	CodeBrandAligned         = "brand_aligned_domain"
	CodeSuspiciousLinks      = "suspicious_links"
	CodeURLShortener         = "url_shortener"
	CodeListUnsubscribe      = "list_unsubscribe"
	CodeDangerousAttachment  = "dangerous_attachment_type"
	CodeStatisticalSpam      = "statistical_spam"
	CodeStatisticalHam       = "statistical_ham"
	CodeMachineGenerated     = "machine_generated_domain"
	CodeSenderDigitPattern   = "sender_digit_pattern"
	CodeSpamVertical         = "spam_vertical_content"
	CodeProfileMismatch      = "profile_mismatch"
	CodeFakeEndorsement      = "fake_endorsement"
	CodeTvShowAbuse          = "tv_show_domain_abuse"
	CodeProfileTopicMatch    = "profile_topic_match"
	CodeProfileOffTopic      = "profile_offtopic_campaign"
	CodeLocalModelPrefix     = "local_model_"
)

var (
	// Generic pressure/threat language (German + English).
	urgencyTerms = regexp.MustCompile(`(?i)(sofort handeln|dringend|konto gesperrt|gesperrt|sperrung|letzte warnung|letztmalig|läuft ab|frist (läuft|endet)|zahlung fehlgeschlagen|fehlgeschlagene zahlung|ungewöhnliche aktivität|act now|urgent|immediately|suspended|final notice|last warning)`)
	// Reward/bait language.
	rewardTerms = regexp.MustCompile(`(?i)(gewinn|gewonnen|lotterie|glücklich|glücks|gratis|kostenlos|geschenk|überraschung wartet|(eine |ihre )?überraschung für sie|paket (ist |steht )?(für sie )?bereit|auto-paket|bonus|prämie|exklusiv (für|nur)|sie gehören zu den|jackpot|cashback|erstattung|gutschein wartet)`)
	// Requests to confirm identity or credentials.
	verifyTerms = regexp.MustCompile(`(?i)(bestätigen sie|bitte bestätigen|identität bestätigen|identität verifizieren|verifizieren sie|konto aktualisieren|daten aktualisieren|angaben aktualisieren|passwort bestätigen|klicken sie (hier|unten)|jetzt anmelden und|confirm (your|now)|verify (your|now)|update (your|now))`)
	// Financial pressure patterns.
	financialTerms = regexp.MustCompile(`(?i)(steht noch aus|ausstehende zahlung|bitte.{0,20}begleichen|offene (rechnung|forderung)|unbezahlt|unbeglichen|offener betrag|mahnung|inkasso|zahlungserinnerung|überprüfen sie ihr konto|kontoaktualisierung erforderlich)`)
	trackingURL    = regexp.MustCompile(`(?i)(bit\.ly|tinyurl\.com|t\.co|cutt\.ly|rb\.gy)/`)
	digitRun       = regexp.MustCompile(`[0-9]{4,}`)
	digitAnywhere  = regexp.MustCompile(`[0-9]`)
	doubleBang     = regexp.MustCompile(`!{2,}`)
	uppercaseWord  = regexp.MustCompile(`\b[A-ZÄÖÜ]{6,}\b`)
	// Emojis/pictographs in the subject. Genuine formal or business mail rarely
	// uses them, while spam and marketing do, so they are a weak indicator. The
	// ranges start at U+2600; ordinary text, digits and umlauts never match.
	emojiInSubject = regexp.MustCompile(`[\x{2600}-\x{27BF}\x{2B00}-\x{2BFF}\x{1F000}-\x{1FAFF}]`)
	// The receiving mail server prepended its own spam tag to the subject
	// ("*** Spam ***", "[Spam]", "Spam:"). Legitimate mail rarely carries these.
	serverSpamMarker = regexp.MustCompile(`(?i)(\*{2,}\s*spam\s*\*{2,}|\[\s*spam\s*\]|^\s*spam\s*[:\-])`)
	// Sextortion/blackmail: threats to publish intimate material or to contact
	// the victim's family. Generic wording, no brand or data lists.
	blackmailTerms = regexp.MustCompile(`(?i)(skandal|erpress|kompromittier|video\s+wird.{0,30}(gesendet|geschickt|veröffentlicht|weitergegeben)|an\s+(ihre|deine)\s+familie|ich\s+habe\s+(dich|sie)\s+(gefilmt|aufgenommen|mitgeschnitten))`)
	// Klassische Massen-Spam-Vertikalen: typisches Kampagnen-Vokabular der
	// großen kommerziellen Spam-Wellen (Deutsch + Englisch). Das sind
	// GENERISCHE Muster ganzer Industriezweige (Diät-Pillen, Krypto-Anlage,
	// Potenz, Krankenkassen-Lockangebote, Wallet-KYC, Kaltakquise) – keine
	// nutzerspezifischen Blocklisten. Die Formulierungen sind absichtlich
	// kampagnentypisch gewählt (Mehrwort-Kombinationen, Anpreisungen), damit
	// normale Fach-/Privatpost nicht zufällig trifft.
	dietHealthTerms = regexp.MustCompile(`(?i)(bauchfett|(schnell|einfach|mühelos|leicht) abnehmen|abnehmen (ohne|leicht|schnell)|(diät|darm|detox)[- ]?(spray|gummis|gummies|pillen|kapseln|kur|diät|tropfen)|keto[- ]?gummies|ozempic|semaglutid|mounjaro|mounjaslim|glp-?1|medicare[- ]?kit|wundermittel|geheimwaffe gegen|fettverbrenn|schlank (in|ohne|werden)|wohlgefühl|(besser|wieder|gesund|tiefer) schlafen|schlaf (verdient|komfort|qualität)|durchschlafen|ein- und durchschlafen)`)
	cryptoInvestTerms = regexp.MustCompile(`(?i)(bitcoin[- ]?(anlage|investment|handel|spark)?|krypto[- ]?(investment|anlage|handel)|mit krypto|crypto[- ]?(investment|trading)|day[- ]?trading|trading[- ]?(bot|system|software|plattform)|anleger (werden|erhalten|profitieren)|vermögen (aufbauen|verdoppeln|vermehren)|rendite von|250 (eur|€|dollar|usd)|einstieg verpasst|geldstress|anlageberater (ruft|meldet)|finanzielle freiheit|passives einkommen|systemtreffern|klug investieren|geld kommt täglich|mit (erfolg|system) (investieren|anlegen)|ai für sie arbeiten|gewinn(e|system) (mit|durch) (ki|ai|crypto|krypto))`)
	potencyTerms = regexp.MustCompile(`(?i)(potenz|erektion|libido|viagra|standvermögen|ausdauernder (sex|liebhaber)|spontaner, ausdauernder|(wieder|mehr|puren?) lust|knistern|glied vergröß|(sexuell|sex) (leistung|aktiver|ausdauer)|bettnachbarin|mach sie (völlig )?sprachlos|voll auskosten|länger (durchhalten|im bett)|befriedigender sex)`)
	insurerBaitTerms = regexp.MustCompile(`(?i)((medicare|krankenkasse|krankenversicherung|gesundheitsvorteil|zahnzusatz|bonusprogramm).{0,80}(kostenlos|gratis|geschenk|bereit|wartet|bestätigt|auswahl wurde|vorteil|kit|testen|versand))|((kostenlos testen|gratis|geschenk|ihre auswahl wurde bestätigt|auswahl wurde bestätigt|wartet auf sie|bereit zum versand|gesundheitsvorteil wartet).{0,80}(medicare|krankenkasse|gesundheitsvorteil|zahnzusatz|kit|bestätigt|bereit|wartet))`)
	// Prominenten-/Experten-Endorsements und TV-Verweise sind eine eigene
	// Betrugsmasche („Empfohlen von Dr. Hirschhausen“, „wie im TV gesehen“).
	fakeEndorsementTerms = regexp.MustCompile(`(?i)(empfohlen (von|bei) (dr\.?|prof\.?|ärzten|experten|medizinern|apothekern)|bekannt aus (tv|fernsehen|presse|medien)|wie (im (tv|fernsehen)|gesehen) (gesehen|vorgestellt)?|im (tv|fernsehen) (gesehen|vorgestellt)|stiftung warentest (hat )?(empfiehlt|getestet|bestätigt)|dr\.? (hirschhausen|nguyen-kim)|fernsehshow|verbraucherschutz (warnt|empfiehlt))`)
	// Missbrauch bekannter TV-Show-Namen in der ABSENDERDOMAIN („Die Höhle der
	// Löwen“-Investment-Masche). Offizielle Sender versenden nie von solchen
	// Domains; Umlaut-Varianten (ö/o/oe) werden abgedeckt.
	tvShowAbuseTerms = regexp.MustCompile(`(?i)(die.?h(o|oe|ö)hle.?d(e|ä)r.?(l(o|oe|ö)w(e|ä)n)|shark.?tank|dragons?.?den|bauer.?sucht.?frau|dschungel.?camp)`)
	walletKycTerms = regexp.MustCompile(`(?i)(kyc[- ]?(anforderung|verifizierung|prüfung|pflicht)|wallet[- ]?verifizier|(verifizieren|bestätigen) sie (ihre|jetzt ihre)? ?wallet|wallet (verifizieren|bestätigen|sichern)|(krypto|coin).{0,40}(verifizierung|kyc)|phantom[- ]?wallet)`)
	coldAcquisitionTerms = regexp.MustCompile(`(?i)(ich habe mir.{0,40}angeschaut|hast du etwas dagegen|haben sie etwas dagegen|darf ich (es |dir |ihnen )?.{0,30}(unverbindlich )?(zusenden|zuschicken|schicken)|unverbindlich (zusenden|zuschicken|zukommen)|förderfähig (aufgesetzt|gemacht|gemeldet)|bis zu \d{1,2} prozent.{0,40}(vom staat|zurück|erstatt|förder)|kosten.{0,20}vom staat zurück)`)
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
	return r.ClassifyFull(msg, profile, nil, features, scorer)
}

// ClassifyFull ist die vollständige Pipeline inklusive der KI-kompilierten
// Profil-Indikatoren (nil = keine Kompilierung vorhanden/aktiv).
func (r *Rules) ClassifyFull(msg domain.MessageFeatures, profile domain.MailboxProfile, indicators *domain.ProfileIndicators, features map[string]int, scorer StatisticalScorer) domain.Classification {
	evidence := make([]domain.Evidence, 0, 8)

	strongTrust := r.stageTrust(msg, profile, &evidence)
	r.stageDeny(msg, profile, &evidence)
	r.stageSenderIntegrity(msg, &evidence)
	r.stageAuthentication(msg, &evidence)
	r.stageContent(msg, &evidence)
	r.stageVerticals(msg, profile, &evidence)
	r.stageProfileIndicators(msg, indicators, &evidence)
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
	// A known brand's own domain with an aligned envelope (Return-Path on the
	// same domain or a brand sibling) and passed authentication really is that
	// brand: the envelope is set by the delivering MTA and cannot be forged in
	// the message headers. This is DMARC-style alignment as a trust signal.
	if msg.AuthenticationPassed && isCanonicalBrand(msg.FromDomain) && envelopeAligned(msg.FromDomain, msg.ReturnPath) {
		strongTrust = true
		*evidence = append(*evidence, domain.Evidence{Group: "authentication", Code: CodeBrandAligned, Weight: -0.7, Summary: "Bekannte Marke mit passender Envelope-Adresse und bestandener Authentifizierung"})
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
		*evidence = append(*evidence, domain.Evidence{Group: "sender_integrity", Code: CodeMachineGenerated, Weight: 0.6, Summary: "Absenderdomain wirkt automatisch zusammengesetzt (unübliche Länge/Ziffern/Vokalanteil)"})
	}
	// An incoming message whose From claims the account owner's own domain is
	// very likely spoofed (e.g. sextortion forging the victim's address). The
	// owner rarely mails themselves; a known correspondent is excluded.
	if own := strings.ToLower(strings.TrimSpace(msg.OwnDomain)); own != "" && senderDomain == own && !msg.KnownCorrespondent {
		*evidence = append(*evidence, domain.Evidence{Group: "sender_integrity", Code: CodeSpoofedSender, Weight: 0.45, Summary: "Absender gibt die eigene Domain an – bei eingehender Mail wahrscheinlich gefälscht (Spoofing)"})
	}
	// Envelope sender (Return-Path) versus From: the envelope is set by the
	// delivering MTA while From is free text a spammer chooses arbitrarily. A
	// domain mismatch outside of mailing lists is a classic spoofing hint.
	if ExtractDomain(msg.ReturnPath) != "" && !envelopeAligned(senderDomain, msg.ReturnPath) && !msg.ListUnsubscribe {
		*evidence = append(*evidence, domain.Evidence{Group: "sender_integrity", Code: CodeSenderMismatch, Weight: 0.35, Summary: "Return-Path weicht vom From-Absender ab (Envelope-/Header-Mismatch)"})
	}
	// Brand impersonation: the domain carries a commonly forged trademark but
	// is not the brand's own domain (e.g. rfcrecouvpaypal.com).
	if brand, ok := impersonatedBrand(senderDomain); ok {
		*evidence = append(*evidence, domain.Evidence{Group: "sender_integrity", Code: CodeBrandImpersonation, Weight: 0.5, Summary: "Absenderdomain imitiert eine bekannte Marke (" + brand + ")"})
	}
	// Namen bekannter TV-Shows in der Absenderdomain sind ein klassisches
	// Scam-Muster (Investment-/Produktbetrug „wie im Fernsehen“); offizielle
	// Sender versenden niemals von solchen Domains.
	if tvShowAbuseTerms.MatchString(senderDomain) {
		*evidence = append(*evidence, domain.Evidence{Group: "sender_integrity", Code: CodeTvShowAbuse, Weight: 0.55, Summary: "Absenderdomain missbraucht den Namen einer bekannten TV-Show (Scam-Muster)"})
	}
}

// brandTokens lists commonly impersonated trademarks with their legitimate
// domains. A sender domain that contains a token without being one of the
// brand's own domains is treated as impersonation. This is a small,
// transparent, built-in list of famous marks for a heuristic - not an external
// blacklist of bad domains; it only adds evidence and never blocks or deletes.
var brandTokens = []struct {
	token string
	own   []string
}{
	{"paypal", []string{"paypal.com", "paypal.de"}},
	{"amazon", []string{"amazon.de", "amazon.com", "amazonaws.com"}},
	{"adac", []string{"adac.de"}},
	{"telekom", []string{"telekom.de", "t-online.de"}},
	{"google", []string{"google.com", "google.de", "googlemail.com"}},
	{"microsoft", []string{"microsoft.com", "live.com", "outlook.com"}},
	{"apple", []string{"apple.com", "icloud.com"}},
	{"netflix", []string{"netflix.com"}},
	{"klarna", []string{"klarna.com", "klarna.de"}},
	{"vodafone", []string{"vodafone.de", "vodafone.com"}},
	{"sparkasse", []string{"sparkasse.de"}},
	{"dhl", []string{"dhl.de", "dhl.com"}},
	{"strato", []string{"strato.de", "strato.com"}},
	{"lidl", []string{"lidl.de", "lidl.com"}},
}

func impersonatedBrand(senderDomain string) (string, bool) {
	// Domains of known brands (built-in list or Wikidata export) are never
	// impersonating themselves or anyone else.
	if isCanonicalBrand(senderDomain) {
		return "", false
	}
	for _, brand := range brandTokens {
		if !strings.Contains(senderDomain, brand.token) {
			continue
		}
		legitimate := false
		for _, own := range brand.own {
			if senderDomain == own || strings.HasSuffix(senderDomain, "."+own) {
				legitimate = true
				break
			}
		}
		if !legitimate {
			return brand.token, true
		}
	}
	// Extended brand list derived from the CC0 Wikidata company export.
	ensureBrandData()
	for _, brand := range derivedBrandTokens {
		if strings.Contains(senderDomain, brand.token) {
			return brand.token, true
		}
	}
	return "", false
}

// isCanonicalBrand reports whether the domain is one of a known brand's own
// domains (or a subdomain thereof), e.g. google.com or accounts.google.com.
// Besides the small built-in list this includes the Wikidata-derived export
// of legitimate company/brand domains (CC0, see data/legit_domains.txt).
func isCanonicalBrand(senderDomain string) bool {
	for _, brand := range brandTokens {
		for _, own := range brand.own {
			if senderDomain == own || strings.HasSuffix(senderDomain, "."+own) {
				return true
			}
		}
	}
	return isLegitCompanyDomain(senderDomain)
}

// envelopeAligned implements DMARC-style relaxed alignment between the From
// domain and the Return-Path envelope domain. Subdomains of each other and
// brand siblings (google.com vs googlemail.com) count as aligned; everything
// else is a mismatch worth evidence.
func envelopeAligned(fromDomain, returnPath string) bool {
	returnDomain := strings.ToLower(strings.TrimSpace(ExtractDomain(returnPath)))
	fromDomain = strings.ToLower(strings.TrimSpace(fromDomain))
	if returnDomain == "" || fromDomain == "" {
		return false
	}
	if returnDomain == fromDomain || strings.HasSuffix(returnDomain, "."+fromDomain) || strings.HasSuffix(fromDomain, "."+returnDomain) {
		return true
	}
	return sameBrand(fromDomain, returnDomain)
}

// sameBrand reports whether both domains belong to the same known brand.
func sameBrand(first, second string) bool {
	match := func(brand struct {
		token string
		own   []string
	}) bool {
		fm, sm := false, false
		for _, own := range brand.own {
			if first == own || strings.HasSuffix(first, "."+own) {
				fm = true
			}
			if second == own || strings.HasSuffix(second, "."+own) {
				sm = true
			}
		}
		return fm && sm
	}
	for _, brand := range brandTokens {
		if match(struct {
			token string
			own   []string
		}(brand)) {
			return true
		}
	}
	ensureBrandData()
	for _, brand := range derivedBrandTokens {
		if match(brand) {
			return true
		}
	}
	return false
}

// machineGeneratedLabel reports whether a domain label looks concatenated or
// randomly generated. Pronounceable domains of normal length (including long
// personal or company names) are not flagged.
func machineGeneratedLabel(senderDomain string) bool {
	labels := strings.Split(strings.TrimSuffix(senderDomain, "."), ".")
	// Ignore the TLD; analyze the remaining labels.
	for _, rawLabel := range labels[:max(len(labels)-1, 0)] {
		// Reine Rechtsform-Segmente (GmbH, AG, …) zählen nicht mit: Deutsche
		// Firmendomains koppeln legitimately Wörter und Rechtsformen
		// (mittelstand-gmbh.de ist aussprechbar und echt), während
		// Spam-Domains ihre Zufallssegmente behalten (techniker-tkund7germany).
		label := stripLegalFormSegments(rawLabel)
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

// companyLegalForms sind reine Rechtsform-/Verbindungssegmente, die vor der
// Zufälligkeitsanalyse entfernt werden.
var companyLegalForms = map[string]bool{}

func init() {
	for _, word := range strings.Fields("gmbh ag kg ohg ug eg mbh co und ltd inc llc plc bv sa srl oy ab as") {
		companyLegalForms[word] = true
	}
}

func stripLegalFormSegments(label string) string {
	if !strings.Contains(label, "-") {
		return label
	}
	segments := strings.Split(label, "-")
	kept := make([]string, 0, len(segments))
	for _, segment := range segments {
		if !companyLegalForms[strings.ToLower(segment)] {
			kept = append(kept, segment)
		}
	}
	return strings.Join(kept, "-")
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
	// Fake-Endorsements: erfundene Arzt-/Promi-/TV-Empfehlungen sind ein
	// generisches Betrugsmerkmal kommerzieller Spam-Wellen.
	if fakeEndorsementTerms.MatchString(combined) {
		*evidence = append(*evidence, domain.Evidence{Group: "content", Code: CodeFakeEndorsement, Weight: 0.35, Summary: "Bewerbung mit Prominenten-/Experten-/TV-Endorsement (typisches Betrugsmuster)"})
	}
	if doubleBang.MatchString(subject) || uppercaseWord.MatchString(subject) {
		*evidence = append(*evidence, domain.Evidence{Group: "content", Code: CodeSubjectAnomaly, Weight: 0.15, Summary: "Auffällige Zeichensetzung oder Schreibung im Betreff"})
	}
	// Emojis in the subject are a weak mismatch signal: a message that poses as
	// a serious/formal notice but uses emojis is unusual. Low weight keeps
	// legitimate newsletters/personal mail below the threshold on its own.
	if emojiInSubject.MatchString(subject) {
		*evidence = append(*evidence, domain.Evidence{Group: "content", Code: CodeSubjectEmoji, Weight: 0.15, Summary: "Emojis im Betreff – unüblich für seriöse/formelle Nachrichten"})
	}
	// The receiving mail server already tagged the subject as spam. A strong,
	// wording-independent signal; legitimate mail rarely carries these markers.
	if serverSpamMarker.MatchString(subject) {
		*evidence = append(*evidence, domain.Evidence{Group: "content", Code: CodeServerMarkedSpam, Weight: 0.55, Summary: "Eingangs-Server hat die Mail als Spam markiert (Betreff-Marker)"})
	}
	// Sextortion/blackmail: threatening to publish material or contact family.
	if blackmailTerms.MatchString(combined) {
		*evidence = append(*evidence, domain.Evidence{Group: "content", Code: CodeBlackmail, Weight: 0.7, Summary: "Erpressungs-/Sextortion-Drohung (Veröffentlichung, Familie)"})
	}
}

// spamVertical beschreibt eine generische Massen-Spam-Kampagnenkategorie:
// Indikator-Regex, lesbarer Name und das Gewicht bei einem Treffer.
type spamVertical struct {
	name   string
	terms  *regexp.Regexp
	weight float64
}

var spamVerticals = []spamVertical{
	{"Diät-/Gesundheitsprodukt-Kampagne", dietHealthTerms, 0.4},
	{"Krypto-/Investment-Werbeversprechen", cryptoInvestTerms, 0.4},
	{"Potenz-/Erotik-Anpreisung", potencyTerms, 0.45},
	{"Krankenkassen-/Medicare-Lockangebot", insurerBaitTerms, 0.4},
	{"Wallet-/KYC-Phishing", walletKycTerms, 0.4},
	{"Kaltakquise mit Förder-/Zuschuss-Versprechen", coldAcquisitionTerms, 0.35},
}

// profileStopWords sind Standardbegriffe aus dem Einrichtungsassistenten und
// allgemeine Geschäfts-/Sprachbegriffe. Sie zählen nicht als Profil-Vokabular,
// damit Boilerplate die Profilvergleichung nicht wirkungslos macht.
var profileStopWords = map[string]bool{}

func init() {
	for _, word := range strings.Fields(`kundenanfragen kunden kunde lieferanten lieferant newsletter automatische automatisch kontomails konto konten portale portal mail mails postfach erwartete erwartet unternehmen bestellungen bestellung rechnungen rechnung versand rückfragen anfrage anfragen logins login bestätigungen bestätigung systemmails erwünschte erwünscht deutsch englisch sprachen sprache` ) {
		profileStopWords[word] = true
	}
}

// stageVerticals prüft den Inhalt gegen generische Spam-Kampagnenkategorien
// (absenderunabhängiges Massenmailing-Vokabular). Der erste Treffer wird als
// ein Beweisstück gewertet; mehrere Vertikalen würden dieselbe Mail doppelt
// bestrafen. Zusätzlich wird bei einem Treffer geprüft, ob das hinterlegte
// Postfachprofil inhaltlich überhaupt berührt wird: Eine Diät-Pillen-Welle ist
// für ein Design-Postfach ein weit stärkeres Signal als für eine Apotheke.
// Beides sind reine Evidence-Signale – generisch, erklärbar, keine
// nutzerspezifische Blockliste.
func (r *Rules) stageVerticals(msg domain.MessageFeatures, profile domain.MailboxProfile, evidence *[]domain.Evidence) {
	combined := msg.Subject + " " + msg.Text
	if len(combined) > 64<<10 {
		combined = combined[:64<<10]
	}
	hit := false
	for _, vertical := range spamVerticals {
		if !vertical.terms.MatchString(combined) {
			continue
		}
		*evidence = append(*evidence, domain.Evidence{Group: "content", Code: CodeSpamVertical, Weight: vertical.weight, Summary: "Inhalt passt zur generischen Spam-Kampagnenkategorie: " + vertical.name})
		hit = true
		break
	}
	if !hit {
		return
	}
	if profileOffTopic(profile, combined) {
		*evidence = append(*evidence, domain.Evidence{Group: "profile", Code: CodeProfileMismatch, Weight: 0.3, Summary: "Keinerlei inhaltliche Überschneidung mit dem hinterlegten Postfachprofil"})
	}
}

// profileOffTopic meldet, ob die Nachricht NULL Themenwörter mit dem
// Mailboxprofil teilt. Bewusst konservativ: erst ab drei aussagekräftigen
// Profilwörtern wird geurteilt, Boilerplate zählt nicht, und geordnet wird
// nur exakt oder per Substring (Wortstamm-Näherung). Der Check läuft NIE
// allein, sondern nur zusammen mit einem Kampagnentreffer – ungewöhnliche,
// aber legitime Post (Rechnungen, Benachrichtigungen) bleibt unangetastet.
func profileOffTopic(profile domain.MailboxProfile, combined string) bool {
	split := func(input string) []string {
		return strings.FieldsFunc(strings.ToLower(input), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		})
	}
	profileWords := map[string]bool{}
	collect := func(input string) {
		for _, word := range split(input) {
			if len([]rune(word)) >= 4 && !profileStopWords[word] {
				profileWords[word] = true
			}
		}
	}
	collect(profile.Purpose)
	collect(profile.Industry)
	for _, item := range profile.ExpectedMailTypes {
		collect(item)
	}
	for _, item := range profile.WantedNewsletters {
		collect(item)
	}
	for _, item := range profile.LegitimateAutomated {
		collect(item)
	}
	if len(profileWords) < 3 {
		return false // Profil zu dünn für ein Relevanzurteil
	}
	messageWords := map[string]bool{}
	for _, word := range split(combined) {
		messageWords[word] = true
	}
	lower := strings.ToLower(combined)
	for word := range profileWords {
		if messageWords[word] {
			return false
		}
		if len([]rune(word)) >= 6 && strings.Contains(lower, word) {
			return false
		}
	}
	return true
}

// stageProfileIndicators wertet die KI-kompilierten Profil-Indikatoren aus
// (pro Postfach in der DB, erzeugt aus dem Profiltext des Nutzers, jederzeit
// neu generierbar/deaktivierbar). Erwartungsthemen belegen Relevanz (schwaches
// Negativ-Signal); profilspezifische Kampagnenkategorien zählen erst bei
// zwei Term-Treffern oder einem einzelnen sehr spezifischen (langen) Term.
// Das Kompilat ist validiert und begrenzt, bleibt aber Modell-Output: es wird
// strikt als Heuristik behandelt, niemals als Block-Grund.
func (r *Rules) stageProfileIndicators(msg domain.MessageFeatures, indicators *domain.ProfileIndicators, evidence *[]domain.Evidence) {
	if indicators == nil {
		return
	}
	combined := strings.ToLower(msg.Subject + " " + msg.Text)
	if len(combined) > 64<<10 {
		combined = combined[:64<<10]
	}
	expectedHits := 0
	for _, topic := range indicators.ExpectedTopics {
		term := strings.ToLower(strings.TrimSpace(topic))
		if len([]rune(term)) < 4 || !strings.Contains(combined, term) {
			continue
		}
		expectedHits++
		if expectedHits >= 2 {
			break
		}
	}
	if expectedHits >= 2 {
		*evidence = append(*evidence, domain.Evidence{Group: "profile", Code: CodeProfileTopicMatch, Weight: -0.25, Summary: "Inhalt trifft KI-kompilierte Erwartungsthemen dieses Postfachprofils"})
	}
	for _, group := range indicators.UnexpectedTopics {
		hits := 0
		longest := 0
		for _, term := range group.Terms {
			normalized := strings.ToLower(strings.TrimSpace(term))
			if len([]rune(normalized)) < 4 || !strings.Contains(combined, normalized) {
				continue
			}
			hits++
			if runes := len([]rune(normalized)); runes > longest {
				longest = runes
			}
		}
		if hits >= 2 || (hits == 1 && longest >= 10) {
			*evidence = append(*evidence, domain.Evidence{Group: "profile", Code: CodeProfileOffTopic, Weight: 0.35, Summary: "Inhalt passt zur profilspezifischen Fremdkampagne „" + group.Name + "“ (KI-kompiliert)"})
			break // höchstens ein Kategorie-Beweis, wie bei den eingebauten Vertikalen
		}
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
	// Strip trailing junk (dots, dashes) that spammers append to imitate a
	// legitimate domain while evading exact matches.
	return strings.TrimRight(parts[1], ".-")
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
