package service

import (
	"context"
	"testing"
	"time"

	"github.com/RHM-GER/Mailmune/internal/domain"
	"github.com/RHM-GER/Mailmune/internal/learning"
	"github.com/RHM-GER/Mailmune/internal/mailbox/imaptest"
)

// TestExportTransferLearningAndProfile covers the portable export document:
// learning exports carry only the statistical model, profile exports carry
// the model plus the mailbox profile, and unknown kinds are rejected.
func TestExportTransferLearningAndProfile(t *testing.T) {
	server := imaptest.New(t, rev2Caps())
	svc, db := newTestService(t, server)
	ctx := context.Background()

	account := domain.AccountConfig{
		ID: "exp-1", Name: "Export Postfach", Host: "h", Port: 993, Username: "u",
		SecretRef: "imap/exp-1", InboxFolder: "INBOX", SpamFolder: "AI_SPAM_FILTER",
		SafetyMode: domain.SafetySafe, Enabled: true, DryRun: true,
		Profile: domain.MailboxProfile{Purpose: "Kundenanfragen", TrustedDomains: []string{"kunde.example"}},
	}
	if err := db.UpsertAccount(ctx, account); err != nil {
		t.Fatal(err)
	}
	model := learning.NewModel()
	model.Train(map[string]int{"kunde": 2}, learning.ClassHam)
	model.Train(map[string]int{"gewinn": 3}, learning.ClassSpam)
	if err := db.SaveLearningModel(ctx, "exp-1", model); err != nil {
		t.Fatal(err)
	}

	l, err := svc.ExportTransfer(ctx, "exp-1", "learning")
	if err != nil {
		t.Fatal(err)
	}
	if l["app"] != "mailmune" || l["kind"] != "learning" || l["version"] != 1 || l["account"] != "Export Postfach" {
		t.Fatalf("learning export meta wrong: %+v", l)
	}
	if _, ok := l["learning"]; !ok {
		t.Fatal("learning export must contain the model")
	}
	if _, ok := l["profile"]; ok {
		t.Fatal("learning export must NOT contain the profile")
	}

	p, err := svc.ExportTransfer(ctx, "exp-1", "profile")
	if err != nil {
		t.Fatal(err)
	}
	profile, ok := p["profile"].(domain.MailboxProfile)
	if !ok || profile.Purpose != "Kundenanfragen" || len(profile.TrustedDomains) != 1 {
		t.Fatalf("profile export wrong: %+v", p["profile"])
	}
	if _, ok := p["learning"]; !ok {
		t.Fatal("profile export should include the trained model")
	}

	if _, err := svc.ExportTransfer(ctx, "exp-1", "alles"); err == nil {
		t.Fatal("unknown kind must fail")
	}
	if _, err := svc.ExportTransfer(ctx, "gibt-es-nicht", "learning"); err == nil {
		t.Fatal("unknown account must fail")
	}
	_ = time.Now()
}

// TestExportTransferUntrainedOmitsModel asserts an account without training
// data exports no empty model payload.
func TestExportTransferUntrainedOmitsModel(t *testing.T) {
	server := imaptest.New(t, rev2Caps())
	svc, db := newTestService(t, server)
	ctx := context.Background()
	account := domain.AccountConfig{
		ID: "exp-2", Name: "Leer", Host: "h", Port: 993, Username: "u",
		SecretRef: "imap/exp-2", InboxFolder: "INBOX", SpamFolder: "AI_SPAM_FILTER",
		SafetyMode: domain.SafetySafe, Enabled: true, DryRun: true,
	}
	if err := db.UpsertAccount(ctx, account); err != nil {
		t.Fatal(err)
	}
	export, err := svc.ExportTransfer(ctx, "exp-2", "learning")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := export["learning"]; ok {
		t.Fatalf("untrained account must not export a model: %+v", export)
	}
}
