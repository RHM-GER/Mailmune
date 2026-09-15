package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/RHM-GER/Mailmune/internal/domain"
)

func TestAutomationRequiresCalibration(t *testing.T) {
	svc, db := newCalibrationService(t)
	ctx := context.Background()
	account := domain.AccountConfig{
		ID: "gate-1", Name: "gate", Host: "imap.example", Port: 993, Username: "u",
		SecretRef: "imap/gate-1", InboxFolder: "INBOX", SentFolder: "Sent",
		SpamFolder: "AI_SPAM_FILTER", SafetyMode: domain.SafetySafe, Enabled: true, DryRun: true,
	}
	if err := db.UpsertAccount(ctx, account); err != nil {
		t.Fatal(err)
	}
	if err := svc.secrets.Set(account.SecretRef, "secret"); err != nil {
		t.Fatal(err)
	}

	// Without any reviewed decisions, leaving dry run must be refused.
	account.DryRun = false
	_, err := svc.SaveAccount(ctx, SaveAccountRequest{Account: account})
	if !errors.Is(err, ErrAutomationNotCalibrated) {
		t.Fatalf("expected calibration gate, got %v", err)
	}

	// Even 20 clean confirmations at lower scores are not enough: precision
	// is measured at the 0.98 auto-move threshold.
	for uid := uint32(1); uid <= 20; uid++ {
		seedReviewedDecision(t, db, "gate-1", uid, 0.70, domain.StatusConfirmed)
	}
	_, err = svc.SaveAccount(ctx, SaveAccountRequest{Account: account})
	if !errors.Is(err, ErrAutomationNotCalibrated) {
		t.Fatalf("expected gate with low-score confirmations, got %v", err)
	}

	// 20 confirmations at >= 0.98 prove precision -> automation allowed.
	for uid := uint32(101); uid <= 120; uid++ {
		seedReviewedDecision(t, db, "gate-1", uid, 0.99, domain.StatusConfirmed)
	}
	saved, err := svc.SaveAccount(ctx, SaveAccountRequest{Account: account})
	if err != nil {
		t.Fatalf("automation should be allowed after calibration: %v", err)
	}
	if saved.DryRun {
		t.Fatal("account still in dry run after successful activation")
	}

	// Turning automation back off is always allowed.
	saved.DryRun = true
	off, err := svc.SaveAccount(ctx, SaveAccountRequest{Account: saved})
	if err != nil || !off.DryRun {
		t.Fatalf("disabling automation must always work: %+v err=%v", off, err)
	}
}

func TestNewAccountCannotSkipDryRun(t *testing.T) {
	svc, _ := newCalibrationService(t)
	ctx := context.Background()
	_, err := svc.SaveAccount(ctx, SaveAccountRequest{
		Account: domain.AccountConfig{
			Name: "neu", Host: "imap.example", Port: 993, Username: "u",
			SafetyMode: domain.SafetySafe, Enabled: true, DryRun: false,
		},
		Password: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	accounts, _ := svc.Accounts(ctx)
	if len(accounts) != 1 || !accounts[0].DryRun {
		t.Fatalf("new account must start in dry run: %+v", accounts)
	}
	_ = time.Now
}
