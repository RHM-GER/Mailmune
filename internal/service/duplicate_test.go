package service

import (
	"context"
	"testing"

	"github.com/RHM-GER/Mailmune/internal/domain"
	"github.com/RHM-GER/Mailmune/internal/mailbox/imaptest"
)

// TestSaveAccountUpdateRejectsDuplicateIdentity: Ein Update, das Zugangsdaten
// auf ein bereits verbundenes Postfach ändert, muss abgelehnt werden –
// sonst entstehen Doppel-Profile mit geteiltem Lernen.
func TestSaveAccountUpdateRejectsDuplicateIdentity(t *testing.T) {
	server := imaptest.New(t, rev2Caps())
	svc, db := newTestService(t, server)
	ctx := context.Background()

	first := domain.AccountConfig{
		ID: "dup-1", Name: "Eins", Host: "imap.example", Port: 993, Username: "user@example",
		SecretRef: "imap/dup-1", InboxFolder: "INBOX", SpamFolder: "AI_SPAM_FILTER",
		SafetyMode: domain.SafetySafe, Enabled: true, DryRun: true,
	}
	if _, err := svc.SaveAccount(ctx, SaveAccountRequest{Account: first, Password: "pw1"}); err != nil {
		t.Fatal(err)
	}
	second := first
	second.ID = "dup-2"
	second.Name = "Zwei"
	second.Host = "imap.other.example"
	second.Username = "user@other.example"
	if _, err := svc.SaveAccount(ctx, SaveAccountRequest{Account: second, Password: "pw2"}); err != nil {
		t.Fatal(err)
	}

	// Update von dup-2 auf die Identität von dup-1 muss scheitern.
	second.Host = "IMAP.example" // case-insensitiver Treffer
	second.Username = " User@Example "
	if _, err := svc.SaveAccount(ctx, SaveAccountRequest{Account: second}); err == nil {
		t.Fatal("duplicate identity via update must be rejected")
	}

	// Update auf sich selbst (gleiche ID) bleibt erlaubt.
	current, err := db.Account(ctx, "dup-2")
	if err != nil {
		t.Fatal(err)
	}
	current.Name = "Zwei umbenannt"
	if _, err := svc.SaveAccount(ctx, SaveAccountRequest{Account: current}); err != nil {
		t.Fatalf("self-update must stay allowed: %v", err)
	}
}
