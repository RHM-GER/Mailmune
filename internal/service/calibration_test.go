package service

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/RHM-GER/Mailmune/internal/domain"
	"github.com/RHM-GER/Mailmune/internal/mailbox"
	"github.com/RHM-GER/Mailmune/internal/store"
)

func newCalibrationService(t *testing.T) (*Service, *store.SQLite) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "cal.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewWithMailbox(db, &memSecrets{values: map[string]string{}}, mailbox.NewClient()), db
}

// seedReviewedDecision stores a decision with a fixed score and review status.
func seedReviewedDecision(t *testing.T, db *store.SQLite, accountID string, uid uint32, score float64, status domain.DecisionStatus) {
	t.Helper()
	now := time.Now()
	decision := domain.MessageDecision{
		ID: fmt.Sprintf("cal-%d", uid), AccountID: accountID, UIDValidity: 1, UID: uid,
		MessageIDHash: fmt.Sprintf("h-%d", uid), OriginFolder: "INBOX", CurrentFolder: "INBOX",
		From: "x@example.com", Subject: "s", Score: score, Status: status,
		IdempotencyKey: fmt.Sprintf("scan:%s:1:%d:INBOX", accountID, uid),
		ReceivedAt: now, CreatedAt: now, ReviewedAt: &now,
	}
	if err := db.SaveDecision(context.Background(), decision); err != nil {
		t.Fatal(err)
	}
}

func TestCalibrationReportCountsAndMetrics(t *testing.T) {
	svc, db := newCalibrationService(t)
	ctx := context.Background()
	if err := db.UpsertAccount(ctx, domain.AccountConfig{ID: "cal-1", Name: "cal", Host: "h", Port: 993, Username: "u", SecretRef: "imap/cal-1", InboxFolder: "INBOX", SpamFolder: "AI_SPAM_FILTER", SafetyMode: domain.SafetySafe, Enabled: true, DryRun: true}); err != nil {
		t.Fatal(err)
	}
	// High-score confirmed spam, plus one rejected false alarm at 0.85.
	seedReviewedDecision(t, db, "cal-1", 1, 0.99, domain.StatusConfirmed)
	seedReviewedDecision(t, db, "cal-1", 2, 0.95, domain.StatusConfirmed)
	seedReviewedDecision(t, db, "cal-1", 3, 0.85, domain.StatusRejected)
	seedReviewedDecision(t, db, "cal-1", 4, 0.65, domain.StatusRejected)

	report, err := svc.CalibrationReport(ctx, "cal-1")
	if err != nil {
		t.Fatal(err)
	}
	if report.Reviewed != 4 || report.Confirmed != 2 || report.Rejected != 2 {
		t.Fatalf("counts wrong: %+v", report)
	}
	// At threshold 0.90: predicted spam = {0.99, 0.95}; both confirmed → TP=2, FP=0.
	var at090 domain.ThresholdMetric
	for _, metric := range report.Thresholds {
		if metric.Threshold == 0.90 {
			at090 = metric
		}
	}
	if at090.TP != 2 || at090.FP != 0 || at090.Precision != 1.0 {
		t.Fatalf("threshold 0.90 metric wrong: %+v", at090)
	}
	// At threshold 0.80: predicted spam = {0.99,0.95,0.85}; 0.85 is rejected → FP=1.
	var at080 domain.ThresholdMetric
	for _, metric := range report.Thresholds {
		if metric.Threshold == 0.80 {
			at080 = metric
		}
	}
	if at080.FP != 1 || at080.TP != 2 {
		t.Fatalf("threshold 0.80 metric wrong: %+v", at080)
	}
}

func TestCalibrationAutoMoveReady(t *testing.T) {
	svc, db := newCalibrationService(t)
	ctx := context.Background()
	if err := db.UpsertAccount(ctx, domain.AccountConfig{ID: "cal-2", Name: "cal", Host: "h", Port: 993, Username: "u", SecretRef: "imap/cal-2", InboxFolder: "INBOX", SpamFolder: "AI_SPAM_FILTER", SafetyMode: domain.SafetySafe, Enabled: true, DryRun: true}); err != nil {
		t.Fatal(err)
	}
	// 20 confirmed at >=0.98, no false alarms → precision 1.0 on a big enough sample.
	for uid := uint32(1); uid <= 20; uid++ {
		seedReviewedDecision(t, db, "cal-2", uid, 0.99, domain.StatusConfirmed)
	}
	report, err := svc.CalibrationReport(ctx, "cal-2")
	if err != nil {
		t.Fatal(err)
	}
	if !report.AutoMoveReady {
		t.Fatalf("expected auto-move ready with 20 clean confirmations: %+v", report)
	}
}

func TestCalibrationNotReadyOnSmallSample(t *testing.T) {
	svc, db := newCalibrationService(t)
	ctx := context.Background()
	if err := db.UpsertAccount(ctx, domain.AccountConfig{ID: "cal-3", Name: "cal", Host: "h", Port: 993, Username: "u", SecretRef: "imap/cal-3", InboxFolder: "INBOX", SpamFolder: "AI_SPAM_FILTER", SafetyMode: domain.SafetySafe, Enabled: true, DryRun: true}); err != nil {
		t.Fatal(err)
	}
	// Only 3 confirmations: precision is perfect but the sample is too small.
	for uid := uint32(1); uid <= 3; uid++ {
		seedReviewedDecision(t, db, "cal-3", uid, 0.99, domain.StatusConfirmed)
	}
	report, _ := svc.CalibrationReport(ctx, "cal-3")
	if report.AutoMoveReady {
		t.Fatal("auto-move must not be ready on a tiny sample")
	}
}

func TestCalibrationNotReadyOnFalseAlarms(t *testing.T) {
	svc, db := newCalibrationService(t)
	ctx := context.Background()
	if err := db.UpsertAccount(ctx, domain.AccountConfig{ID: "cal-4", Name: "cal", Host: "h", Port: 993, Username: "u", SecretRef: "imap/cal-4", InboxFolder: "INBOX", SpamFolder: "AI_SPAM_FILTER", SafetyMode: domain.SafetySafe, Enabled: true, DryRun: true}); err != nil {
		t.Fatal(err)
	}
	// 20 confirmed but 2 false alarms at >=0.98 → precision 20/22 = 0.909 < 0.995.
	for uid := uint32(1); uid <= 20; uid++ {
		seedReviewedDecision(t, db, "cal-4", uid, 0.99, domain.StatusConfirmed)
	}
	seedReviewedDecision(t, db, "cal-4", 100, 0.99, domain.StatusRejected)
	seedReviewedDecision(t, db, "cal-4", 101, 0.99, domain.StatusRejected)
	report, _ := svc.CalibrationReport(ctx, "cal-4")
	if report.AutoMoveReady {
		t.Fatal("auto-move must not be ready when precision is below target")
	}
}

func TestCalibrationEmptyAccount(t *testing.T) {
	svc, db := newCalibrationService(t)
	ctx := context.Background()
	if err := db.UpsertAccount(ctx, domain.AccountConfig{ID: "cal-5", Name: "cal", Host: "h", Port: 993, Username: "u", SecretRef: "imap/cal-5", InboxFolder: "INBOX", SpamFolder: "AI_SPAM_FILTER", SafetyMode: domain.SafetySafe, Enabled: true, DryRun: true}); err != nil {
		t.Fatal(err)
	}
	report, err := svc.CalibrationReport(ctx, "cal-5")
	if err != nil {
		t.Fatal(err)
	}
	if report.Reviewed != 0 || report.AutoMoveReady {
		t.Fatalf("empty account must report nothing ready: %+v", report)
	}
}
