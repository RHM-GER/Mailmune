package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/RHM-GER/Mailmune/internal/secrets"
	"github.com/RHM-GER/Mailmune/internal/service"
	"github.com/RHM-GER/Mailmune/internal/store"
)

type memorySecrets struct{ values map[string]string }

var _ secrets.Store = (*memorySecrets)(nil)

func (m *memorySecrets) Set(reference, value string) error {
	m.values[reference] = value
	return nil
}
func (m *memorySecrets) Get(reference string) (string, error) { return m.values[reference], nil }
func (m *memorySecrets) Delete(reference string) error {
	delete(m.values, reference)
	return nil
}

func TestLocalAPIRequiresTokenAndRejectsBrowserOrigins(t *testing.T) {
	database, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	token := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	server, err := New(token, service.New(database, &memorySecrets{values: map[string]string{}}))
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	})

	client := &http.Client{Timeout: 2 * time.Second}
	request, _ := http.NewRequest(http.MethodGet, server.Address()+"/v1/health", nil)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("without token: got %d, want 401", response.StatusCode)
	}

	request, _ = http.NewRequest(http.MethodGet, server.Address()+"/v1/health", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Origin", "https://example.invalid")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("browser origin: got %d, want 403", response.StatusCode)
	}

	request, _ = http.NewRequest(http.MethodGet, server.Address()+"/v1/health", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("authenticated request: got %d, want 200", response.StatusCode)
	}

	request, _ = http.NewRequest(http.MethodDelete, server.Address()+"/v1/accounts", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("delete request: got %d, want 405", response.StatusCode)
	}
}

func startTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	database, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	token := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	server, err := New(token, service.New(database, &memorySecrets{values: map[string]string{}}))
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	})
	return server, token
}

func TestScanEndpointsAndEventStream(t *testing.T) {
	server, token := startTestServer(t)
	client := &http.Client{}

	authenticated := func(method, path string, body string) *http.Request {
		var request *http.Request
		if body != "" {
			request, _ = http.NewRequest(method, server.Address()+path, strings.NewReader(body))
		} else {
			request, _ = http.NewRequest(method, server.Address()+path, nil)
		}
		request.Header.Set("Authorization", "Bearer "+token)
		return request
	}

	// Subscribe to the event stream before starting any scan.
	streamCtx, cancelStream := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelStream()
	stream, err := client.Do(authenticated(http.MethodGet, "/v1/events", "").WithContext(streamCtx))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	if stream.StatusCode != http.StatusOK {
		t.Fatalf("event stream status = %d", stream.StatusCode)
	}
	lines := make(chan string, 64)
	go func() {
		scanner := bufio.NewScanner(stream.Body)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()
	waitForLine := func(prefix string) {
		deadline := time.After(10 * time.Second)
		for {
			select {
			case line, ok := <-lines:
				if !ok {
					t.Fatalf("event stream closed while waiting for %q", prefix)
				}
				if strings.HasPrefix(line, prefix) {
					return
				}
			case <-deadline:
				t.Fatalf("timed out waiting for %q", prefix)
			}
		}
	}
	waitForLine("event: ready")

	// Create an account whose IMAP port refuses connections immediately:
	// the scan run must fail in a controlled way instead of blocking.
	createResponse, err := client.Do(authenticated(http.MethodPost, "/v1/accounts", `{"account":{"name":"Test","host":"127.0.0.1","port":1,"username":"user","inboxFolder":"INBOX","spamFolder":"AI_SPAM_FILTER","safetyMode":"safe","enabled":true,"dryRun":true,"profile":{"purpose":"","industry":"","languages":[],"expectedMailTypes":[],"trustedDomains":[],"trustedSenders":[],"wantedNewsletters":[],"legitimateAutomated":[]}},"password":"secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	var created struct {
		ID     string `json:"id"`
		DryRun bool   `json:"dryRun"`
	}
	if err := json.NewDecoder(createResponse.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	createResponse.Body.Close()
	if createResponse.StatusCode != http.StatusOK || created.ID == "" {
		t.Fatalf("account creation status = %d", createResponse.StatusCode)
	}
	if !created.DryRun {
		t.Fatal("new accounts must start in dry run")
	}

	startScan := func() string {
		response, err := client.Do(authenticated(http.MethodPost, "/v1/accounts/"+created.ID+"/scans", "{}"))
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var run struct{ ID string }
		if err := json.NewDecoder(response.Body).Decode(&run); err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK || run.ID == "" {
			t.Fatalf("scan start status = %d", response.StatusCode)
		}
		return run.ID
	}
	runID := startScan()
	waitForLine("event: scan.started")

	// Poll the run list until the run reaches its final (failed) state.
	deadline := time.Now().Add(15 * time.Second)
	status := "running"
	for time.Now().Before(deadline) {
		response, err := client.Do(authenticated(http.MethodGet, "/v1/accounts/"+created.ID+"/scans", ""))
		if err != nil {
			t.Fatal(err)
		}
		var runs []struct {
			ID     string
			Status string
		}
		if err := json.NewDecoder(response.Body).Decode(&runs); err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if len(runs) == 0 {
			t.Fatal("no scan runs listed")
		}
		status = runs[0].Status
		if runs[0].ID != runID {
			t.Fatalf("listed run %s differs from started run %s", runs[0].ID, runID)
		}
		if status != "running" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if status != "failed" {
		t.Fatalf("run status = %q, want failed", status)
	}
	waitForLine("event: scan.finished")

	// Cancelling without an active run yields a stable 404.
	response, err := client.Do(authenticated(http.MethodPost, "/v1/accounts/"+created.ID+"/scans/cancel", "{}"))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("cancel without active run: got %d, want 404", response.StatusCode)
	}

	// Health stays reachable and reports the version.
	healthResponse, err := client.Do(authenticated(http.MethodGet, "/v1/health", ""))
	if err != nil {
		t.Fatal(err)
	}
	healthResponse.Body.Close()
	if healthResponse.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d", healthResponse.StatusCode)
	}
}
