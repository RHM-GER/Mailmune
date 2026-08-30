package learning

import (
	"strings"
	"testing"
)

func trainBasics(m *Model) {
	spamText := "gewinn lotterie sofort handeln konto gesperrt krypto investment gratis bonus"
	hamText := "projekt meeting termin absprache angebot vertrag rechnung lieferung"
	for i := 0; i < 12; i++ {
		m.Train(ExtractFeatures("Ihr Gewinn wartet", "spam@lotto-"+string(rune('a'+i%3))+".example", "lotto.example", spamText), ClassSpam)
		m.Train(ExtractFeatures("Projekttermin abstimmen", "kollege@firma.example", "firma.example", hamText), ClassHam)
	}
}

func TestModelSeparatesConfirmedClasses(t *testing.T) {
	m := NewModel()
	trainBasics(m)

	spamScore, ok := m.Score(ExtractFeatures("Gewinn sofort handeln", "unbekannt@lotto.example", "lotto.example", "gratis bonus krypto investment"))
	if !ok || spamScore < 0.85 {
		t.Fatalf("spam score = %.2f ok=%v, want >= 0.85", spamScore, ok)
	}
	hamScore, ok := m.Score(ExtractFeatures("Projekttermin abstimmen", "kollege@firma.example", "firma.example", "meeting vertrag rechnung"))
	if !ok || hamScore > 0.15 {
		t.Fatalf("ham score = %.2f ok=%v, want <= 0.15", hamScore, ok)
	}
}

func TestModelRefusesSingleClassTraining(t *testing.T) {
	m := NewModel()
	for i := 0; i < 10; i++ {
		m.Train(ExtractFeatures("Gewinn", "spam@example.com", "example.com", "lotterie"), ClassSpam)
	}
	if _, ok := m.Score(ExtractFeatures("Gewinn", "x@example.com", "example.com", "lotterie")); ok {
		t.Fatal("model trained on one class must not score")
	}
}

func TestScoreIsDeterministicAndBounded(t *testing.T) {
	m := NewModel()
	trainBasics(m)
	features := ExtractFeatures("Gewinn lotterie", "a@b.example", "b.example", "sofort handeln")
	first, ok := m.Score(features)
	if !ok {
		t.Fatal("expected a score")
	}
	for i := 0; i < 5; i++ {
		again, _ := m.Score(features)
		if again != first {
			t.Fatalf("score not deterministic: %f vs %f", again, first)
		}
	}
	if first < 0 || first > 1 {
		t.Fatalf("score out of range: %f", first)
	}
}

func TestExtractFeaturesIsBoundedAndReproducible(t *testing.T) {
	text := strings.Repeat("einzigartigestoken ", 2000)
	features := ExtractFeatures("Betreff", "absender@example.com", "example.com", text)
	if len(features) > maxTokensPerMessage+4 {
		t.Fatalf("feature set too large: %d", len(features))
	}
	again := ExtractFeatures("Betreff", "absender@example.com", "example.com", text)
	if len(features) != len(again) {
		t.Fatal("feature extraction not reproducible")
	}
	for token, count := range features {
		if again[token] != count {
			t.Fatalf("token %q differs between runs", token)
		}
		if count > maxOccurrencesPerTok {
			t.Fatalf("token %q exceeds occurrence cap: %d", token, count)
		}
	}
	if _, ok := features["dom:example.com"]; !ok {
		t.Fatal("domain marker missing")
	}
	if _, ok := features["snd:absender@example.com"]; !ok {
		t.Fatal("sender marker missing")
	}
}

func TestResetClearsState(t *testing.T) {
	m := NewModel()
	trainBasics(m)
	m.Reset()
	if m.Trained() != 0 {
		t.Fatalf("trained = %d after reset", m.Trained())
	}
	if _, ok := m.Score(ExtractFeatures("Gewinn", "a@b.example", "b.example", "lotterie")); ok {
		t.Fatal("reset model must not score")
	}
}
