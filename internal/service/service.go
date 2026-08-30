package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/RHM-GER/Mailmune/internal/classifier"
	"github.com/RHM-GER/Mailmune/internal/domain"
	"github.com/RHM-GER/Mailmune/internal/mailbox"
	"github.com/RHM-GER/Mailmune/internal/provider"
	"github.com/RHM-GER/Mailmune/internal/secrets"
	"github.com/RHM-GER/Mailmune/internal/store"
)

type Service struct {
	store   *store.SQLite
	secrets secrets.Store
	mailbox *mailbox.Client
	rules   *classifier.Rules
	ollama  *provider.Ollama
}

type SaveAccountRequest struct {
	Account  domain.AccountConfig `json:"account"`
	Password string               `json:"password,omitempty"`
}
type ScanResult struct {
	Processed  int      `json:"processed"`
	Candidates int      `json:"candidates"`
	Moved      int      `json:"moved"`
	DryRun     bool     `json:"dryRun"`
	Warnings   []string `json:"warnings"`
}

func New(db *store.SQLite, secretStore secrets.Store) *Service {
	return &Service{store: db, secrets: secretStore, mailbox: mailbox.NewClient(), rules: classifier.NewRules(), ollama: provider.NewOllama("")}
}

func (s *Service) Accounts(ctx context.Context) ([]domain.AccountConfig, error) {
	return s.store.ListAccounts(ctx)
}

func (s *Service) SaveAccount(ctx context.Context, request SaveAccountRequest) (domain.AccountConfig, error) {
	a := request.Account
	now := time.Now().UTC()
	if a.ID == "" {
		a.ID = uuid.NewString()
		a.CreatedAt = now
	}
	a.UpdatedAt = now
	if a.Port == 0 {
		a.Port = 993
	}
	if a.InboxFolder == "" {
		a.InboxFolder = "INBOX"
	}
	if a.SpamFolder == "" {
		a.SpamFolder = "AI_SPAM_FILTER"
	}
	if a.SafetyMode == "" {
		a.SafetyMode = domain.SafetySafe
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
	} else if _, err := s.secrets.Get(a.SecretRef); err != nil {
		return a, errors.New("password is required for a new account")
	}
	if err := s.store.UpsertAccount(ctx, a); err != nil {
		return a, err
	}
	return a, nil
}

func (s *Service) TestAccount(ctx context.Context, id string) (mailbox.ConnectionInfo, error) {
	a, err := s.store.Account(ctx, id)
	if err != nil {
		return mailbox.ConnectionInfo{}, err
	}
	password, err := s.secrets.Get(a.SecretRef)
	if err != nil {
		return mailbox.ConnectionInfo{}, err
	}
	return s.mailbox.TestConnection(ctx, a, password)
}

func (s *Service) Scan(ctx context.Context, id string) (ScanResult, error) {
	a, err := s.store.Account(ctx, id)
	if err != nil {
		return ScanResult{}, err
	}
	password, err := s.secrets.Get(a.SecretRef)
	if err != nil {
		return ScanResult{}, err
	}
	messages, _, err := s.mailbox.ScanMetadata(ctx, a, password, a.InboxFolder, 1000, time.Now().AddDate(0, 0, -90))
	if err != nil {
		return ScanResult{}, err
	}
	result := ScanResult{Processed: len(messages), DryRun: a.DryRun}
	for _, message := range messages {
		classification := s.rules.Classify(message, a.Profile)
		if a.OllamaValidated && a.OllamaModel != "" && classification.Score >= 0.25 && classification.Score < 0.98 {
			verdict, modelErr := s.ollama.Classify(ctx, a.OllamaModel, message, a.Profile)
			if modelErr == nil {
				classification.ModelUsed = a.OllamaModel
				classification.ModelValidated = true
				classification.Evidence = append(classification.Evidence, domain.Evidence{Group: "model", Code: "local_model_" + verdict.Class, Weight: verdict.Score, Summary: "Lokales validiertes Modell: " + verdict.Class})
				if verdict.Class == "spam" {
					classification.Score = classification.Score*0.7 + verdict.Score*0.3
					classification.IndependentGroups++
				}
			}
		}
		action := classifier.Decide(a.SafetyMode, classification)
		if action == classifier.ActionIgnore {
			continue
		}
		result.Candidates++
		status := domain.StatusPending
		current := message.Folder
		if action == classifier.ActionMove && !a.DryRun {
			destinationUID, moveErr := s.mailbox.MoveAtomic(ctx, a, password, message.Folder, message.UID, a.SpamFolder)
			if moveErr != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("UID %d nicht verschoben: %v", message.UID, moveErr))
			} else {
				status = domain.StatusMoved
				current = a.SpamFolder
				result.Moved++
				if destinationUID != 0 {
					message.UID = destinationUID
				}
			}
		}
		hash := sha256.Sum256([]byte(strings.ToLower(message.MessageID)))
		decision := domain.MessageDecision{ID: uuid.NewString(), AccountID: a.ID, UIDValidity: message.UIDValidity, UID: message.UID, MessageIDHash: hex.EncodeToString(hash[:]), OriginFolder: message.Folder, CurrentFolder: current, From: message.From, Subject: message.Subject, Score: classification.Score, Status: status, Evidence: classification.Evidence, ModelVersion: classification.ModelUsed, IdempotencyKey: fmt.Sprintf("scan:%s:%d:%d:%s", a.ID, message.UIDValidity, message.UID, message.Folder), ReceivedAt: message.ReceivedAt, CreatedAt: time.Now().UTC()}
		if err := s.store.SaveDecision(ctx, decision); err != nil {
			return result, err
		}
	}
	a.LastScanAt = ptrTime(time.Now().UTC())
	a.UpdatedAt = time.Now().UTC()
	_ = s.store.UpsertAccount(ctx, a)
	return result, nil
}

func (s *Service) Decisions(ctx context.Context, filter store.DecisionFilter) ([]domain.MessageDecision, error) {
	return s.store.ListDecisions(ctx, filter)
}
func (s *Service) Review(ctx context.Context, request domain.ReviewRequest) error {
	return s.store.ApplyReview(ctx, request)
}
func (s *Service) Summary(ctx context.Context) (domain.DashboardSummary, error) {
	return s.store.Summary(ctx)
}
func (s *Service) Models(ctx context.Context) ([]string, error) { return s.ollama.Models(ctx) }
func (s *Service) Purge(ctx context.Context) (int64, error) {
	return s.store.PurgeReadableMetadata(ctx, time.Now().AddDate(0, 0, -180))
}
func ptrTime(value time.Time) *time.Time { return &value }
