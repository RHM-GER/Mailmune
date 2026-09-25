package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RHM-GER/Mailmune/internal/domain"
)

// TestClassifySendsCompiledProfilePrompt beweist, dass der KI-kompilierte
// Profil-Prompt tatsächlich im Modell-Request landet (und ohne Kompilat
// nicht mitgeschickt wird).
func TestClassifySendsCompiledProfilePrompt(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		verdict, _ := json.Marshal(map[string]any{"class": "spam", "score": 0.9, "reasonCodes": []string{}})
		_ = json.NewEncoder(w).Encode(map[string]string{"response": string(verdict)})
	}))
	defer server.Close()

	o, err := NewOllama(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	learned := &LearnedContext{ProfilePrompt: "Dieses Postfach gehoert einer Design-Agentur; Diaet-Werbung ist fremd."}
	if _, err := o.Classify(context.Background(), "m", domain.MessageFeatures{Subject: "x"}, domain.MailboxProfile{}, learned); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "mailboxProfileNotes") || !strings.Contains(body, "Design-Agentur") {
		t.Fatalf("Profil-Prompt fehlt im Modell-Request: %s", body)
	}

	body = ""
	if _, err := o.Classify(context.Background(), "m", domain.MessageFeatures{Subject: "x"}, domain.MailboxProfile{}, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "mailboxProfileNotes") {
		t.Fatal("ohne Kompilat darf mailboxProfileNotes nicht gesendet werden")
	}
}
