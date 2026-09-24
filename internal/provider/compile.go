package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/RHM-GER/Mailmune/internal/domain"
)

// compileInstruction ist der Meta-Prompt der Profil-Kompilierung: Das lokale
// Modell leitet aus dem (vom Nutzer frei beschreibbaren) Postfachprofil zwei
// Dinge ab – einen Profil-Prompt für die Klassifizierung und ein strikt
// begrenztes Indikator-Set für die deterministischen Regeln. Die Ausgabe wird
// vollständig validiert; kein Teil davon wird als Instruktion vertraut.
const compileInstruction = `You compile a mailbox profile into configuration for a local spam filter.
The profile describes what legitimate mail for this mailbox looks like (business, purpose, expected mail types, free-text notes from the owner).
Derive from it, in German:
1. "prompt": one short paragraph (max 800 chars) for an email classifier describing this mailbox: which topics/senders are expected here and which content is clearly foreign.
2. "expectedTopics": 6-24 short literal words or phrases (2-40 chars) that typically appear in LEGITIMATE mail of this profile.
3. "unexpectedTopics": up to 8 mass-mailing campaign categories this profile would never legitimately receive, each with a "name" and 3-12 literal indicator terms (2-40 chars). Think in generic campaign categories (diet products, crypto investment schemes, potency ads, sweepstakes, fake invoices, unsolicited acquisition...), tailored to what this mailbox does NOT deal with.
4. "notes": one sentence explaining the compilation for the mailbox owner.
Strict rules for all terms: literal substrings only (no regex, no wildcards, no punctuation tricks); generic campaign vocabulary; never person names; never the profile's own domains or partners; never single common words that legitimately appear in normal business mail. A term must be specific enough that a hit is strong evidence of a foreign campaign.
Respond with strict JSON only: {"prompt": string, "expectedTopics": [string], "unexpectedTopics": [{"name": string, "terms": [string]}], "notes": string}`

// CompiledProfile ist das validierte Ergebnis einer Profil-Kompilierung.
type CompiledProfile struct {
	Prompt     string
	Indicators domain.ProfileIndicators
}

// CompileProfile fragt das lokale Modell nach einem Profil-Kompilat. Der
// Aufruf ist deterministisch (Temperatur 0), strukturiert (JSON-Schema) und
// das Ergebnis wird hart validiert und begrenzt, bevor es gespeichert wird.
func (o *Ollama) CompileProfile(ctx context.Context, model string, profile domain.MailboxProfile) (CompiledProfile, error) {
	if model == "" {
		return CompiledProfile{}, errors.New("model is required")
	}
	input := map[string]any{
		"task": "compile-mailbox-profile-v1",
		"profile": map[string]any{
			"purpose":           bounded(profile.Purpose, 800),
			"industry":          bounded(profile.Industry, 400),
			"context":           bounded(profile.Context, 2400),
			"expectedMailTypes": profile.ExpectedMailTypes,
			"languages":         profile.Languages,
		},
	}
	format := map[string]any{
		"type":     "object",
		"required": []string{"prompt", "expectedTopics", "unexpectedTopics"},
		"properties": map[string]any{
			"prompt":         map[string]any{"type": "string"},
			"expectedTopics": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 24},
			"unexpectedTopics": map[string]any{
				"type": "array", "maxItems": 8,
				"items": map[string]any{
					"type":     "object",
					"required": []string{"name", "terms"},
					"properties": map[string]any{
						"name":  map[string]any{"type": "string"},
						"terms": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 12},
					},
					"additionalProperties": false,
				},
			},
			"notes": map[string]any{"type": "string"},
		},
		"additionalProperties": false,
	}
	raw, err := o.rawGenerate(ctx, model, compileInstruction+"\n"+mustJSON(input), format, 3072)
	if err != nil {
		return CompiledProfile{}, err
	}
	return ParseCompiledProfile(raw)
}

// ParseCompiledProfile validiert und begrenzt die rohe Modellausgabe. Zu
// kurze, zu lange oder leere Einträge werden verworfen; bleibt zu wenig
// brauchbare Substanz, gilt die Kompilierung als fehlgeschlagen.
func ParseCompiledProfile(raw string) (CompiledProfile, error) {
	object := extractJSONObject(strings.TrimSpace(raw))
	if object == "" {
		return CompiledProfile{}, errors.New("model returned no JSON object")
	}
	var parsed struct {
		Prompt           string                  `json:"prompt"`
		ExpectedTopics   []string                `json:"expectedTopics"`
		UnexpectedTopics []domain.UnexpectedTopic `json:"unexpectedTopics"`
		Notes            string                  `json:"notes"`
	}
	if err := json.Unmarshal([]byte(object), &parsed); err != nil {
		return CompiledProfile{}, fmt.Errorf("compiled profile invalid: %w", err)
	}

	clean := func(term string) string {
		term = strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, strings.TrimSpace(term))
		return term
	}
	validTerm := func(term string) bool {
		runes := []rune(term)
		return len(runes) >= 2 && len(runes) <= 40
	}

	out := CompiledProfile{}
	out.Prompt = clean(bounded(parsed.Prompt, 4000))
	for _, topic := range parsed.ExpectedTopics {
		if len(out.Indicators.ExpectedTopics) >= 24 {
			break
		}
		if topic = clean(topic); validTerm(topic) {
			out.Indicators.ExpectedTopics = append(out.Indicators.ExpectedTopics, topic)
		}
	}
	for _, group := range parsed.UnexpectedTopics {
		if len(out.Indicators.UnexpectedTopics) >= 8 {
			break
		}
		name := clean(bounded(group.Name, 60))
		terms := make([]string, 0, len(group.Terms))
		for _, term := range group.Terms {
			if len(terms) >= 12 {
				break
			}
			if term = clean(term); validTerm(term) {
				terms = append(terms, term)
			}
		}
		if name == "" || len(terms) < 2 {
			continue // unbrauchbare Gruppe verwerfen
		}
		out.Indicators.UnexpectedTopics = append(out.Indicators.UnexpectedTopics, domain.UnexpectedTopic{Name: name, Terms: terms})
	}
	out.Indicators.Notes = clean(bounded(parsed.Notes, 500))

	if out.Prompt == "" || len(out.Indicators.ExpectedTopics) < 3 || len(out.Indicators.UnexpectedTopics) < 1 {
		return CompiledProfile{}, errors.New("model returned too few usable indicators; try again or use a stronger model")
	}
	return out, nil
}

func mustJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}
