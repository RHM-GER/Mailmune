package domain

import "time"

type SafetyMode string

const (
	SafetyConfirmAll SafetyMode = "confirm_all"
	SafetySafe       SafetyMode = "safe"
	SafetyAggressive SafetyMode = "aggressive"
)

type DecisionStatus string

const (
	StatusPending   DecisionStatus = "pending"
	StatusMoved     DecisionStatus = "moved"
	StatusConfirmed DecisionStatus = "confirmed"
	StatusRejected  DecisionStatus = "rejected"
	StatusDeferred  DecisionStatus = "deferred"
)

type AccountConfig struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	Host            string         `json:"host"`
	Port            int            `json:"port"`
	Username        string         `json:"username"`
	SecretRef       string         `json:"secretRef"`
	InboxFolder     string         `json:"inboxFolder"`
	SentFolder      string         `json:"sentFolder"`
	SpamFolder      string         `json:"spamFolder"`
	SafetyMode      SafetyMode     `json:"safetyMode"`
	OllamaModel     string         `json:"ollamaModel,omitempty"`
	OllamaValidated bool           `json:"ollamaValidated"`
	Enabled         bool           `json:"enabled"`
	DryRun          bool           `json:"dryRun"`
	LastScanAt      *time.Time     `json:"lastScanAt,omitempty"`
	CreatedAt       time.Time      `json:"createdAt"`
	UpdatedAt       time.Time      `json:"updatedAt"`
	Profile         MailboxProfile `json:"profile"`
}

type MailboxProfile struct {
	Purpose             string   `json:"purpose"`
	Industry            string   `json:"industry"`
	Languages           []string `json:"languages"`
	ExpectedMailTypes   []string `json:"expectedMailTypes"`
	TrustedDomains      []string `json:"trustedDomains"`
	TrustedSenders      []string `json:"trustedSenders"`
	DeniedSenders       []string `json:"deniedSenders"`
	DeniedDomains       []string `json:"deniedDomains"`
	DeniedKeywords      []string `json:"deniedKeywords"`
	WantedNewsletters   []string `json:"wantedNewsletters"`
	LegitimateAutomated []string `json:"legitimateAutomated"`
}

type AttachmentMetadata struct {
	Filename string `json:"filename"`
	MIMEType string `json:"mimeType"`
	Size     int64  `json:"size"`
}

type MessageFeatures struct {
	AccountID            string               `json:"accountId"`
	UIDValidity          uint32               `json:"uidValidity"`
	UID                  uint32               `json:"uid"`
	Folder               string               `json:"folder"`
	MessageID            string               `json:"messageId"`
	From                 string               `json:"from"`
	FromDomain           string               `json:"fromDomain"`
	ReplyTo              string               `json:"replyTo"`
	Subject              string               `json:"subject"`
	Text                 string               `json:"-"`
	ReceivedAt           time.Time            `json:"receivedAt"`
	ListUnsubscribe      bool                 `json:"listUnsubscribe"`
	AuthenticationPassed bool                 `json:"authenticationPassed"`
	AuthenticationFailed bool                 `json:"authenticationFailed"`
	KnownCorrespondent   bool                 `json:"knownCorrespondent"`
	URLCount             int                  `json:"urlCount"`
	Attachments          []AttachmentMetadata `json:"attachments"`
}

type Evidence struct {
	Group   string  `json:"group"`
	Code    string  `json:"code"`
	Weight  float64 `json:"weight"`
	Summary string  `json:"summary"`
}

type Classification struct {
	Score             float64    `json:"score"`
	Evidence          []Evidence `json:"evidence"`
	IndependentGroups int        `json:"independentGroups"`
	StrongTrustSignal bool       `json:"strongTrustSignal"`
	ModelUsed         string     `json:"modelUsed,omitempty"`
	ModelValidated    bool       `json:"modelValidated"`
	RecommendedAction string     `json:"recommendedAction"`
}

type MessageDecision struct {
	ID             string         `json:"id"`
	AccountID      string         `json:"accountId"`
	UIDValidity    uint32         `json:"uidValidity"`
	UID            uint32         `json:"uid"`
	MessageIDHash  string         `json:"messageIdHash"`
	OriginFolder   string         `json:"originFolder"`
	CurrentFolder  string         `json:"currentFolder"`
	From           string         `json:"from"`
	Subject        string         `json:"subject"`
	Score          float64        `json:"score"`
	Status         DecisionStatus `json:"status"`
	Evidence       []Evidence     `json:"evidence"`
	ModelVersion   string         `json:"modelVersion,omitempty"`
	IdempotencyKey string       `json:"idempotencyKey"`
	ReceivedAt     time.Time    `json:"receivedAt"`
	CreatedAt      time.Time    `json:"createdAt"`
	ReviewedAt     *time.Time   `json:"reviewedAt,omitempty"`
	TrainedAt      *time.Time   `json:"trainedAt,omitempty"`
}

type ReviewAction string

const (
	ReviewConfirm ReviewAction = "confirm"
	ReviewReject  ReviewAction = "reject"
	ReviewDefer   ReviewAction = "defer"
)

type ReviewRequest struct {
	DecisionIDs    []string     `json:"decisionIds"`
	Action         ReviewAction `json:"action"`
	IdempotencyKey string       `json:"idempotencyKey"`
}

type DashboardSummary struct {
	Accounts      int     `json:"accounts"`
	Pending       int     `json:"pending"`
	Moved         int     `json:"moved"`
	Confirmed     int     `json:"confirmed"`
	Rejected      int     `json:"rejected"`
	FalsePositive float64 `json:"falsePositiveRate"`
	ProcessedWeek int     `json:"processedWeek"`
}

// FolderSyncState is the persisted UID synchronization state of one folder.
// A change of UIDVALIDITY invalidates every stored UID and requires a safe
// re-sync from UID 1; blind continuation is forbidden.
type FolderSyncState struct {
	AccountID   string    `json:"accountId"`
	Folder      string    `json:"folder"`
	UIDValidity uint32    `json:"uidValidity"`
	LastUID     uint32    `json:"lastUid"`
	LastSyncAt  time.Time `json:"lastSyncAt"`
}

type ScanStatus string

const (
	ScanRunning     ScanStatus = "running"
	ScanCompleted   ScanStatus = "completed"
	ScanCancelled   ScanStatus = "cancelled"
	ScanFailed      ScanStatus = "failed"
	ScanInterrupted ScanStatus = "interrupted"
)

type ScanRun struct {
	ID             string     `json:"id"`
	AccountID      string     `json:"accountId"`
	Status         ScanStatus `json:"status"`
	Folder         string     `json:"folder"`
	Processed      int        `json:"processed"`
	EstimatedTotal int        `json:"estimatedTotal"`
	StartedAt      time.Time  `json:"startedAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	FinishedAt     *time.Time `json:"finishedAt,omitempty"`
	Error          string     `json:"error,omitempty"`
}

// LearningBaseline records the provenance of an optional, opt-in global
// learner imported from an external corpus. It is kept separate from the
// per-account confirmed learning so it can be inspected and deleted without
// touching user-confirmed knowledge.
type LearningBaseline struct {
	ID           string    `json:"id"`
	Version      int       `json:"version"`
	Source       string    `json:"source"`
	License      string    `json:"license"`
	CorpusRows   int       `json:"corpusRows"`
	SpamMessages uint64    `json:"spamMessages"`
	HamMessages  uint64    `json:"hamMessages"`
	ImportedAt   time.Time `json:"importedAt"`
}

// MoveDirection distinguishes a move into the spam folder from a restore of a
// false positive back to its origin folder.
type MoveDirection string

const (
	MoveToSpam  MoveDirection = "to_spam"
	MoveRestore MoveDirection = "restore"
)

// MoveState is one node of the explicit, crash-safe move state machine.
// Every transition has preconditions, a timestamp and an idempotency key; a
// repeated run after a crash never produces a second physical move.
type MoveState string

const (
	MovePlanned        MoveState = "planned"
	MoveMoving         MoveState = "moving"
	MoveMoved          MoveState = "moved"
	MoveConfirmed      MoveState = "confirmed"
	MoveRestoring      MoveState = "restoring"
	MoveRestored       MoveState = "restored"
	MoveFailedRetry    MoveState = "failed_retryable"
	MoveFailedTerminal MoveState = "failed_terminal"
)

// MoveOperation is one guarded IMAP move tracked by the state machine.
type MoveOperation struct {
	ID                string        `json:"id"`
	AccountID         string        `json:"accountId"`
	DecisionID        string        `json:"decisionId,omitempty"`
	Direction         MoveDirection `json:"direction"`
	OriginFolder      string        `json:"originFolder"`
	OriginUID         uint32        `json:"originUid"`
	OriginUIDValidity uint32        `json:"originUidValidity"`
	TargetFolder      string        `json:"targetFolder"`
	State             MoveState     `json:"state"`
	DestUID           uint32        `json:"destUid,omitempty"`
	DestUIDValidity   uint32        `json:"destUidValidity,omitempty"`
	MessageIDHash     string        `json:"messageIdHash,omitempty"`
	Attempts          int           `json:"attempts"`
	LastError         string        `json:"lastError,omitempty"`
	CreatedAt         time.Time     `json:"createdAt"`
	UpdatedAt         time.Time     `json:"updatedAt"`
}
