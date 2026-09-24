package provider

import (
	"strings"
	"testing"
)

const validCompileResponse = `{
	"prompt": "Dieses Postfach gehört einer Design-Agentur. Erwartet werden Kundenanfragen zu Logo, Webseite und Video. Fremd sind Diät-Produkte, Krypto-Anlagen und Potenzmittel.",
	"expectedTopics": ["design", "logo", "webseite", "angebot", "rechnungsentwurf", "videoproduktion"],
	"unexpectedTopics": [
		{"name": "Diät-Produkte", "terms": ["bauchfett", "abnehmen ohne", "diät spray", "keto gummies"]},
		{"name": "Krypto-Anlage", "terms": ["bitcoin anlage", "250 eur einzahlen", "trading bot"]}
	],
	"notes": "Kompiliert aus dem Agenturbeschreibungs-Profil."
}`

func TestParseCompiledProfileValid(t *testing.T) {
	compiled, err := ParseCompiledProfile(validCompileResponse)
	if err != nil {
		t.Fatalf("valid response rejected: %v", err)
	}
	if !strings.Contains(compiled.Prompt, "Design-Agentur") {
		t.Fatalf("prompt wrong: %q", compiled.Prompt)
	}
	if len(compiled.Indicators.ExpectedTopics) != 6 {
		t.Fatalf("expectedTopics = %d, want 6", len(compiled.Indicators.ExpectedTopics))
	}
	if len(compiled.Indicators.UnexpectedTopics) != 2 {
		t.Fatalf("unexpectedTopics = %d, want 2", len(compiled.Indicators.UnexpectedTopics))
	}
	if compiled.Indicators.UnexpectedTopics[0].Name != "Diät-Produkte" || len(compiled.Indicators.UnexpectedTopics[0].Terms) != 4 {
		t.Fatalf("first group wrong: %+v", compiled.Indicators.UnexpectedTopics[0])
	}
}

func TestParseCompiledProfileHandlesFencesAndPreamble(t *testing.T) {
	wrapped := "Here is the compilation:\n```json\n" + validCompileResponse + "\n```\nDone."
	if _, err := ParseCompiledProfile(wrapped); err != nil {
		t.Fatalf("fenced response rejected: %v", err)
	}
}

func TestParseCompiledProfileRejectsUnusable(t *testing.T) {
	cases := map[string]string{
		"kein JSON":        "I cannot do that.",
		"leere Indikatoren": `{"prompt": "x", "expectedTopics": [], "unexpectedTopics": []}`,
		"zu wenig Topics":  `{"prompt": "x", "expectedTopics": ["design"], "unexpectedTopics": [{"name":"a","terms":["bb","cc"]}]}`,
		"Gruppe zu dünn":   `{"prompt": "x", "expectedTopics": ["design","logo","video"], "unexpectedTopics": [{"name":"a","terms":["bb"]}]}`,
		"ohne Prompt":      `{"prompt": "", "expectedTopics": ["design","logo","video"], "unexpectedTopics": [{"name":"a","terms":["bb","cc"]}]}`,
	}
	for name, raw := range cases {
		if _, err := ParseCompiledProfile(raw); err == nil {
			t.Errorf("%s: unusable response must be rejected", name)
		}
	}
}

func TestParseCompiledProfileEnforcesBounds(t *testing.T) {
	longTerm := strings.Repeat("a", 80)
	raw := `{
		"prompt": "` + strings.Repeat("p", 5000) + `",
		"expectedTopics": ["design", "logo", "video", "` + longTerm + `", "x"],
		"unexpectedTopics": [
			{"name": "` + strings.Repeat("n", 90) + `", "terms": ["bauchfett", "abnehmen", "` + longTerm + `"]},
			{"name": "", "terms": ["aa", "bb"]}
		],
		"notes": "` + strings.Repeat("z", 900) + `"
	}`
	compiled, err := ParseCompiledProfile(raw)
	if err != nil {
		t.Fatalf("bounded response rejected: %v", err)
	}
	if len([]rune(compiled.Prompt)) > 4000 {
		t.Fatal("prompt not bounded")
	}
	for _, topic := range compiled.Indicators.ExpectedTopics {
		if len([]rune(topic)) > 40 || len([]rune(topic)) < 2 {
			t.Fatalf("unbounded expected topic %q", topic)
		}
	}
	if len(compiled.Indicators.UnexpectedTopics) != 1 {
		t.Fatalf("empty-name group must be dropped: %+v", compiled.Indicators.UnexpectedTopics)
	}
	group := compiled.Indicators.UnexpectedTopics[0]
	if len([]rune(group.Name)) > 60 {
		t.Fatal("group name not bounded")
	}
	for _, term := range group.Terms {
		if len([]rune(term)) > 40 {
			t.Fatalf("unbounded term %q", term)
		}
	}
	if len([]rune(compiled.Indicators.Notes)) > 500 {
		t.Fatal("notes not bounded")
	}
}
