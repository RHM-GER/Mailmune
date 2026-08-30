package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RHM-GER/Mailmune/internal/domain"
)

func fakeOllama(t *testing.T, response func() (int, string)) *Ollama {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status, body := response()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	provider, err := NewOllama(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func verdictJSON(class string, score float64) string {
	payload, _ := json.Marshal(map[string]any{"class": class, "score": score, "reasonCodes": []string{"TEST_CODE"}})
	outer, _ := json.Marshal(map[string]string{"response": string(payload)})
	return string(outer)
}

func TestNewOllamaRejectsNonLoopback(t *testing.T) {
	for _, candidate := range []string{"http://10.0.0.5:11434", "http://ollama.example.com", "ftp://127.0.0.1:11434", "http://192.168.1.10:11434"} {
		if _, err := NewOllama(candidate); err == nil {
			t.Fatalf("%s must be rejected", candidate)
		}
	}
	for _, candidate := range []string{"", "http://127.0.0.1:11434", "http://localhost:11434", "http://[::1]:11434"} {
		if _, err := NewOllama(candidate); err != nil {
			t.Fatalf("%s must be accepted: %v", candidate, err)
		}
	}
}

func TestClassifyAcceptsValidVerdict(t *testing.T) {
	provider := fakeOllama(t, func() (int, string) { return http.StatusOK, verdictJSON("spam", 0.91) })
	verdict, err := provider.Classify(context.Background(), "test-model", domain.MessageFeatures{From: "a@b.example", Subject: "Gewinn"}, domain.MailboxProfile{})
	if err != nil {
		t.Fatal(err)
	}
	if verdict.Class != "spam" || verdict.Score != 0.91 {
		t.Fatalf("verdict = %+v", verdict)
	}
}

func TestClassifyRejectsContractViolations(t *testing.T) {
	cases := map[string]func() (int, string){
		"broken json":   func() (int, string) { return http.StatusOK, `{"response": "this is not json"}` },
		"invalid class": func() (int, string) { return http.StatusOK, verdictJSON("virus", 0.9) },
		"score too high": func() (int, string) {
			payload, _ := json.Marshal(map[string]any{"class": "spam", "score": 1.4, "reasonCodes": []string{}})
			outer, _ := json.Marshal(map[string]string{"response": string(payload)})
			return http.StatusOK, string(outer)
		},
		"too many reasons": func() (int, string) {
			payload, _ := json.Marshal(map[string]any{"class": "spam", "score": 0.5, "reasonCodes": []string{"a", "b", "c", "d", "e", "f"}})
			outer, _ := json.Marshal(map[string]string{"response": string(payload)})
			return http.StatusOK, string(outer)
		},
		"server error": func() (int, string) { return http.StatusInternalServerError, "{}" },
	}
	for name, respond := range cases {
		provider := fakeOllama(t, respond)
		if _, err := provider.Classify(context.Background(), "test-model", domain.MessageFeatures{}, domain.MailboxProfile{}); err == nil {
			t.Fatalf("%s: expected rejection", name)
		}
	}
}

func TestParseVerdictDirectly(t *testing.T) {
	if _, err := parseVerdict(`{"class":"ham","score":0.2,"reasonCodes":[]}`); err != nil {
		t.Fatal(err)
	}
	if _, err := parseVerdict(`garbage`); err == nil {
		t.Fatal("garbage must be rejected")
	}
}

func TestCapabilityRunAgainstLocalModel(t *testing.T) {
	requests := 0
	provider := fakeOllama(t, func() (int, string) {
		requests++
		return http.StatusOK, verdictJSON("uncertain", 0.4)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	report, err := provider.RunCapabilityTest(ctx, "test-model")
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed {
		t.Fatalf("capability run must pass on structurally valid answers: %+v", report)
	}
	if len(report.Cases) != len(capabilityCases) {
		t.Fatalf("cases = %d, want %d", len(report.Cases), len(capabilityCases))
	}
	if requests != len(capabilityCases) {
		t.Fatalf("model calls = %d, want %d", requests, len(capabilityCases))
	}
	names := map[string]bool{}
	for _, result := range report.Cases {
		names[result.Name] = true
		if !result.Valid {
			t.Fatalf("case %s invalid: %s", result.Name, result.Error)
		}
	}
	if !names["prompt_injection"] || !names["long_text"] || !names["contradictory_signals"] {
		t.Fatalf("required cases missing: %v", names)
	}
}

func TestCapabilityFailsOnBrokenAnswers(t *testing.T) {
	provider := fakeOllama(t, func() (int, string) { return http.StatusOK, `{"response": "model refuses json"}` })
	report, err := provider.RunCapabilityTest(context.Background(), "test-model")
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed {
		t.Fatal("broken answers must fail the capability test")
	}
	for _, result := range report.Cases {
		if result.Valid || result.Error == "" || !strings.Contains(result.Error, "invalid model JSON") {
			t.Fatalf("case %s should be invalid with JSON error: %+v", result.Name, result)
		}
	}
}
