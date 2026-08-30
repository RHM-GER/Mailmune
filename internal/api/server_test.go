package api

import (
	"context"
	"net/http"
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
