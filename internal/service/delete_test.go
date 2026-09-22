package service

import (
	"context"
	"testing"

	"github.com/RHM-GER/Mailmune/internal/domain"
)

// TestSaveAccountRejectsDuplicateMailbox guards against the "profiles appear
// multiple times" bug: the same mailbox (host + username) must never be
// connected twice, while updating the same account stays possible.
func TestSaveAccountRejectsDuplicateMailbox(t *testing.T) {
	svc, db := newModelService(t, "http://127.0.0.1:1")
	ctx := context.Background()

	request := func(id string) SaveAccountRequest {
		return SaveAccountRequest{
			Account: domain.AccountConfig{
				ID: id, Name: id, Host: "imap.example", Port: 993, Username: "user@example",
				InboxFolder: "INBOX", SentFolder: "Sent", SpamFolder: "AI_SPAM_FILTER",
				SafetyMode: domain.SafetySafe, Enabled: true, DryRun: true,
			},
			Password: "secret",
		}
	}

	if _, err := svc.SaveAccount(ctx, request("dup-1")); err != nil {
		t.Fatal(err)
	}
	// The same mailbox under a new ID must be rejected.
	if _, err := svc.SaveAccount(ctx, request("dup-2")); err == nil {
		t.Fatal("duplicate mailbox was accepted")
	}
	// Updating the same account (same ID) must stay possible.
	if _, err := svc.SaveAccount(ctx, request("dup-1")); err != nil {
		t.Fatalf("update of the same account failed: %v", err)
	}
	accounts, err := db.ListAccounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 1 {
		t.Fatalf("accounts = %d, want 1", len(accounts))
	}
}

// TestDeleteAccountRemovesSecretAndData checks the service-level deletion:
// the profile, its decisions and its keyring secret disappear. Emails on the
// IMAP server are never touched (the mailbox client is not involved at all).
func TestDeleteAccountRemovesSecretAndData(t *testing.T) {
	svc, db := newModelService(t, "http://127.0.0.1:1")
	ctx := context.Background()

	saved, err := svc.SaveAccount(ctx, SaveAccountRequest{
		Account: domain.AccountConfig{
			ID: "del-svc", Name: "Del", Host: "imap.example", Port: 993, Username: "del@example",
			InboxFolder: "INBOX", SentFolder: "Sent", SpamFolder: "AI_SPAM_FILTER",
			SafetyMode: domain.SafetySafe, Enabled: true, DryRun: true,
		},
		Password: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.secrets.Get(saved.SecretRef); err != nil {
		t.Fatalf("secret not stored: %v", err)
	}

	if err := svc.DeleteAccount(ctx, saved.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Account(ctx, saved.ID); err == nil {
		t.Fatal("account still in store after DeleteAccount")
	}
	if _, err := svc.secrets.Get(saved.SecretRef); err == nil {
		t.Fatal("keyring secret survived DeleteAccount")
	}
	// Deleting twice reports not-found instead of panicking.
	if err := svc.DeleteAccount(ctx, saved.ID); err == nil {
		t.Fatal("second DeleteAccount should fail with not-found")
	}
}
