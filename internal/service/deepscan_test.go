package service

import (
	"context"
	"testing"
	"time"

	"github.com/RHM-GER/Mailmune/internal/domain"
	"github.com/RHM-GER/Mailmune/internal/mailbox/imaptest"
	"github.com/RHM-GER/Mailmune/internal/store"
)

func TestDeepScanDue(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.Local)
	today := int(now.Weekday())
	yesterday := (today + 6) % 7
	yesterdayTen := time.Date(now.AddDate(0, 0, -1).Year(), now.AddDate(0, 0, -1).Month(), now.AddDate(0, 0, -1).Day(), 10, 0, 0, 0, time.Local)

	account := func(weekday, hour int, last *time.Time, enabled bool) domain.AccountConfig {
		return domain.AccountConfig{Enabled: enabled, DeepScan: true, DeepScanWeekday: weekday, DeepScanHour: hour, LastDeepScanAt: last}
	}
	ptr := func(v time.Time) *time.Time { return &v }

	cases := []struct {
		name    string
		account domain.AccountConfig
		due     bool
	}{
		{"disabled by default", domain.AccountConfig{Enabled: true}, false},
		{"flag off despite schedule", func() domain.AccountConfig { a := account(today, 10, nil, true); a.DeepScan = false; return a }(), false},
		{"disabled account never due", account(today, 10, nil, false), false},
		{"today earlier hour without last run", account(today, 10, nil, true), true},
		{"today later hour not yet", account(today, 14, nil, true), false},
		{"fulfilled after appointment", account(yesterday, 10, ptr(now.Add(-time.Hour)), true), false},
		{"missed appointment stays due", account(yesterday, 10, ptr(now.AddDate(0, 0, -10)), true), true},
		{"run exactly at appointment counts", account(yesterday, 10, ptr(yesterdayTen), true), false},
		{"invalid weekday", account(7, 10, nil, true), false},
		{"invalid hour", account(today, 24, nil, true), false},
		{"partial schedule weekday only", account(today, -1, nil, true), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := deepScanDue(tc.account, now); got != tc.due {
				t.Fatalf("deepScanDue(%+v) = %v, want %v", tc.account, got, tc.due)
			}
		})
	}
}

func TestDeepScanWindowSkipsOlderMessages(t *testing.T) {
	server := imaptest.New(t, rev2Caps())
	svc, db := newTestService(t, server)
	account := createTestAccount(t, svc, server, "acc-deep")
	ctx := context.Background()
	now := time.Now()

	// One message far outside the default 7-day window, one inside.
	server.AddMessage("INBOX", "alt@example.com", "Alte Nachricht", "Vor langer Zeit", now.AddDate(0, 0, -10))
	server.AddMessage("INBOX", "neu@example.com", "Neue Nachricht", "Diese Woche", now.Add(-time.Hour))

	run, err := svc.StartDeepScan(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != domain.ScanRunning {
		t.Fatalf("run status = %s", run.Status)
	}
	finished := waitForScan(t, svc, account.ID)
	if finished.Status != domain.ScanCompleted {
		t.Fatalf("run: %s (%s)", finished.Status, finished.Error)
	}

	filter := store.DecisionFilter{AccountID: account.ID}
	decisions, err := db.ListDecisions(ctx, filter)
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 {
		t.Fatalf("decisions = %d, want 1 (only the message inside the window)", len(decisions))
	}
	if decisions[0].From != "neu@example.com" {
		t.Fatalf("wrong message processed: %s", decisions[0].From)
	}

	updated, err := db.Account(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LastDeepScanAt == nil {
		t.Fatal("LastDeepScanAt must be set after a successful deep scan")
	}
	if time.Since(*updated.LastDeepScanAt) > time.Minute {
		t.Fatalf("LastDeepScanAt not current: %v", updated.LastDeepScanAt)
	}

	// A second deep scan right after uses the fresh boundary; the recent
	// message is inside the window again but deduplicates by idempotency key.
	if _, err := svc.StartDeepScan(ctx, account.ID); err != nil {
		t.Fatal(err)
	}
	if second := waitForScan(t, svc, account.ID); second.Status != domain.ScanCompleted {
		t.Fatalf("second run: %s (%s)", second.Status, second.Error)
	}
	decisions2, err := db.ListDecisions(ctx, filter)
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions2) != 1 {
		t.Fatalf("decisions after second deep scan = %d, want 1 (deduplicated)", len(decisions2))
	}
}
