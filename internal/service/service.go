package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/RHM-GER/Mailmune/internal/classifier"
	"github.com/RHM-GER/Mailmune/internal/domain"
	"github.com/RHM-GER/Mailmune/internal/events"
	"github.com/RHM-GER/Mailmune/internal/learning"
	"github.com/RHM-GER/Mailmune/internal/mailbox"
	"github.com/RHM-GER/Mailmune/internal/provider"
	"github.com/RHM-GER/Mailmune/internal/secrets"
	"github.com/RHM-GER/Mailmune/internal/store"
)

// Service wires storage, secrets, IMAP access and the scan scheduler
// together. All mail access is read-only unless an account was explicitly
// taken out of dry run.
type Service struct {
	store   *store.SQLite
	secrets secrets.Store
	mailbox *mailbox.Client
	rules   *classifier.Rules
	ollama  *provider.Ollama
	hub     *events.Hub
	scanner *Scanner
}

// SaveAccountRequest creates or updates an account. For idempotent creation
// clients generate the account ID once (e.g. a UUID) and resend the same ID
// on retry; the upsert then cannot duplicate the account.
type SaveAccountRequest struct {
	Account  domain.AccountConfig `json:"account"`
	Password string               `json:"password,omitempty"`
}

// New builds the production service with the default strict IMAP client.
func New(db *store.SQLite, secretStore secrets.Store) *Service {
	return NewWithMailbox(db, secretStore, mailbox.NewClient())
}

// NewWithMailbox allows tests to inject an IMAP client that trusts a
// self-signed test CA. Production code uses New.
func NewWithMailbox(db *store.SQLite, secretStore secrets.Store, client *mailbox.Client) *Service {
	hub := events.NewHub()
	ollama, err := provider.NewOllama("")
	if err != nil {
		// The default endpoint is loopback; anything else is a configuration
		// error the agent must not start with.
		panic(fmt.Errorf("ollama provider: %w", err))
	}
	service := &Service{
		store:   db,
		secrets: secretStore,
		mailbox: client,
		rules:   classifier.NewRules(),
		ollama:  ollama,
		hub:     hub,
	}
	service.scanner = newScanner(db, secretStore, client, service.rules, service.ollama, hub)
	if _, err := service.scanner.RecoverInterrupted(context.Background()); err != nil {
		// The database is local and required; failing here means the agent
		// cannot run anyway.
		panic(fmt.Errorf("recover interrupted scan runs: %w", err))
	}
	return service
}

// Hub exposes the event stream used by the local API.
func (s *Service) Hub() *events.Hub { return s.hub }

func (s *Service) Accounts(ctx context.Context) ([]domain.AccountConfig, error) {
	return s.store.ListAccounts(ctx)
}

func (s *Service) SaveAccount(ctx context.Context, request SaveAccountRequest) (domain.AccountConfig, error) {
	a := request.Account
	now := time.Now().UTC()
	if a.ID == "" {
		a.ID = uuid.NewString()
	}
	isNew := !s.accountExists(ctx, a.ID)
	if isNew {
		a.CreatedAt = now
		// Product rule: new mailboxes always begin in a reading dry run.
		// Automation requires an explicit later change plus confirmation.
		a.DryRun = true
	}
	a.UpdatedAt = now
	if a.Port == 0 {
		a.Port = 993
	}
	if a.InboxFolder == "" {
		a.InboxFolder = "INBOX"
	}
	if a.SentFolder == "" {
		a.SentFolder = "Sent"
	}
	if a.SpamFolder == "" {
		a.SpamFolder = "AI_SPAM_FILTER"
	}
	if a.SafetyMode == "" {
		a.SafetyMode = domain.SafetySafe
	}
	switch a.SafetyMode {
	case domain.SafetyConfirmAll, domain.SafetySafe, domain.SafetyAggressive:
	default:
		return a, errors.New("unsupported safety mode")
	}
	if a.Host == "" || a.Username == "" || a.Name == "" {
		return a, errors.New("name, host and username are required")
	}
	if a.SecretRef == "" {
		a.SecretRef = "imap/" + a.ID
	}
	if request.Password != "" {
		if err := s.secrets.Set(a.SecretRef, request.Password); err != nil {
			return a, fmt.Errorf("store password: %w", err)
		}
	} else if isNew {
		return a, errors.New("password is required for a new account")
	} else if _, err := s.secrets.Get(a.SecretRef); err != nil {
		if errors.Is(err, secrets.ErrNotFound) {
			return a, errors.New("stored password is missing; provide it again")
		}
		return a, err
	}
	if err := s.store.UpsertAccount(ctx, a); err != nil {
		return a, err
	}
	if s.hub != nil {
		s.hub.Publish("account.updated", a)
	}
	return a, nil
}

// accountExists reports whether an account ID is already stored, so an
// update with a known ID is never mistaken for a first-time creation.
func (s *Service) accountExists(ctx context.Context, id string) bool {
	if id == "" {
		return false
	}
	_, err := s.store.Account(ctx, id)
	return err == nil
}

func (s *Service) TestAccount(ctx context.Context, id string) (mailbox.ConnectionInfo, error) {
	a, err := s.store.Account(ctx, id)
	if err != nil {
		return mailbox.ConnectionInfo{}, err
	}
	password, err := s.secrets.Get(a.SecretRef)
	if err != nil {
		if errors.Is(err, secrets.ErrNotFound) {
			return mailbox.ConnectionInfo{}, errors.New("stored password is missing; save the account again")
		}
		return mailbox.ConnectionInfo{}, err
	}
	return s.mailbox.TestConnection(ctx, a, password)
}

// StartScan begins a background scan run (idempotent per account).
func (s *Service) StartScan(ctx context.Context, accountID string) (domain.ScanRun, error) {
	return s.scanner.StartScan(ctx, accountID)
}

// CancelScan requests cancellation of the account's active run.
func (s *Service) CancelScan(ctx context.Context, accountID string) (domain.ScanRun, bool, error) {
	return s.scanner.CancelScan(ctx, accountID)
}

// ScanRuns lists recent runs of an account, newest first.
func (s *Service) ScanRuns(ctx context.Context, accountID string, limit int) ([]domain.ScanRun, error) {
	return s.store.ScanRuns(ctx, accountID, limit)
}

func (s *Service) Decisions(ctx context.Context, filter store.DecisionFilter) ([]domain.MessageDecision, error) {
	return s.store.ListDecisions(ctx, filter)
}

func (s *Service) Review(ctx context.Context, request domain.ReviewRequest) error {
	if err := s.store.ApplyReview(ctx, request); err != nil {
		return err
	}
	if request.Action == domain.ReviewConfirm || request.Action == domain.ReviewReject {
		s.trainFromReview(ctx, request)
	}
	return nil
}

// trainFromReview folds confirmed/rejected decisions into the per-account
// statistical learner. Only decisions that were never trained before are
// used, which keeps retries and restarts idempotent. Training failures never
// break the review itself.
func (s *Service) trainFromReview(ctx context.Context, request domain.ReviewRequest) {
	class := learning.ClassSpam
	if request.Action == domain.ReviewReject {
		class = learning.ClassHam
	}
	decisions, err := s.store.DecisionsByIDs(ctx, request.DecisionIDs)
	if err != nil {
		return
	}
	models := map[string]*learning.Model{}
	dirty := map[string]bool{}
	for _, decision := range decisions {
		if decision.TrainedAt != nil {
			continue
		}
		features, ok, err := s.store.DecisionFeatures(ctx, decision.ID)
		if err != nil || !ok || len(features) == 0 {
			continue
		}
		model, exists := models[decision.AccountID]
		if !exists {
			loaded, err := s.store.LoadLearningModel(ctx, decision.AccountID)
			if err != nil {
				continue
			}
			model = loaded
			models[decision.AccountID] = model
		}
		model.Train(features, class)
		if err := s.store.MarkDecisionTrained(ctx, decision.ID); err != nil {
			continue
		}
		dirty[decision.AccountID] = true
	}
	for accountID := range dirty {
		if err := s.store.SaveLearningModel(ctx, accountID, models[accountID]); err != nil {
			delete(dirty, accountID)
		}
	}
}

func (s *Service) Summary(ctx context.Context) (domain.DashboardSummary, error) {
	return s.store.Summary(ctx)
}

func (s *Service) Models(ctx context.Context) ([]string, error) { return s.ollama.Models(ctx) }

// CapabilityTest runs the fixed capability probe set against a local model.
func (s *Service) CapabilityTest(ctx context.Context, model string) (provider.CapabilityReport, error) {
	return s.ollama.RunCapabilityTest(ctx, model)
}

func (s *Service) Purge(ctx context.Context) (int64, error) {
	return s.store.PurgeReadableMetadata(ctx, time.Now().AddDate(0, 0, -180))
}
