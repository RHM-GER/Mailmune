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
	// AIEnabled ist der Hauptschalter der KI-Filterung pro Postfach: Aus =
	// ausschließlich Regeln/Lernfilter, An = das validierte lokale Modell
	// prüft mit (und die Desktop-App hält Ollama am Laufen).
	AIEnabled       bool           `json:"aiEnabled"`
	Enabled         bool           `json:"enabled"`
	DryRun          bool           `json:"dryRun"`
	// DeepScan enables the weekly AI deep scan; DeepScanWeekday/DeepScanHour
	// schedule it in local time (weekday: 0=Sunday..6=Saturday, hour: 0..23).
	// The flag makes the zero value of the schedule fields unambiguously
	// "off", so old clients never enable a deep scan by omission. The deep
	// scan re-reads every message received since LastDeepScanAt (at most the
	// last 7 days on the first run) and reviews all of them with the local
	// model when one is validated.
	DeepScan        bool           `json:"deepScan"`
	DeepScanWeekday int            `json:"deepScanWeekday"`
	DeepScanHour    int            `json:"deepScanHour"`
	LastDeepScanAt  *time.Time     `json:"lastDeepScanAt,omitempty"`
	LastScanAt      *time.Time     `json:"lastScanAt,omitempty"`
	CreatedAt       time.Time      `json:"createdAt"`
	UpdatedAt       time.Time      `json:"updatedAt"`
	Profile         MailboxProfile `json:"profile"`
}

type MailboxProfile struct {
	Purpose             string   `json:"purpose"`
	Industry            string   `json:"industry"`
	// Context ist der Freitext aus der Einrichtung („Weitere Beschreibung“):
	// ungewöhnliche, aber legitime Mails, Besonderheiten des Postfachs. Er ist
	// die Hauptquelle für die KI-Kompilierung der Profil-Indikatoren.
	Context             string   `json:"context"`
	// Unexpected ist der Klartext des Besitzers darüber, was in diesem Postfach
	// NIEMALS erwartet wird (z. B. „Diät-Werbung, Krypto-Anlagen, Kaltakquise“).
	// Die KI leitet daraus die profilspezifischen Fremdkampagnen ab.
	Unexpected          string   `json:"unexpected"`
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

// UnexpectedTopic ist eine vom lokalen Modell kompilierte Kampagnenkategorie,
// die für DIESES Postfachprofil nicht erwartet wird (z. B. „Diät-Pillen“ für
// eine Design-Agentur). Die Terme sind generische Muster, vom Nutzer im
// Profiltext beschrieben und von der KI abgeleitet – keine harten Blocklisten.
type UnexpectedTopic struct {
	Name  string   `json:"name"`
	Terms []string `json:"terms"`
}

// ProfileIndicators ist der Regel-Teil der KI-Profilkompilierung: erwartete
// Themen (senken den Score bei Treffer) und unerwartete Kampagnenkategorien
// (heben ihn bei Treffer). Alles Literal-Substring-Muster, strikt begrenzt
// und bei der Kompilierung validiert; niemals Regex oder Code.
type ProfileIndicators struct {
	ExpectedTopics   []string          `json:"expectedTopics,omitempty"`
	UnexpectedTopics []UnexpectedTopic `json:"unexpectedTopics,omitempty"`
	Notes            string            `json:"notes,omitempty"`
}

// ProfileModel ist das persistierte, versionierte Ergebnis einer
// KI-Profilkompilierung pro Postfach: der Profil-Prompt für das lokale Modell
// plus die Indikator-Sets für die deterministischen Regeln. SourceHash
// markiert das Kompilat als veraltet, sobald sich der Profiltext ändert.
type ProfileModel struct {
	AccountID  string            `json:"accountId"`
	SourceHash string            `json:"sourceHash"`
	CompiledAt time.Time         `json:"compiledAt"`
	Model      string            `json:"model"`
	Prompt     string            `json:"prompt"`
	Indicators ProfileIndicators `json:"indicators"`
	Enabled    bool              `json:"enabled"`
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
	// OwnDomain is the account owner's domain (derived from the IMAP login).
	// An INCOMING message whose From claims this domain is very likely spoofed
	// (e.g. sextortion that forges the victim's own address).
	OwnDomain            string               `json:"ownDomain"`
	ReplyTo              string               `json:"replyTo"`
	// ReturnPath is the envelope sender (Return-Path header / SMTP MAIL FROM).
	// Unlike the free-text From header it is set by the delivering MTA, so a
	// domain mismatch between the two is a spoofing hint.
	ReturnPath           string               `json:"returnPath"`
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
	// Scanned is the total number of unique messages ever classified (one row
	// per message, deduplicated), so a rescan never double-counts.
	Scanned int `json:"scanned"`
}

// DailyStat is one day of aggregated activity across all accounts. It feeds
// the dashboard charts with real data instead of demo values.
type DailyStat struct {
	Day       string `json:"day"`
	Processed int    `json:"processed"`
	Moved     int    `json:"moved"`
	Confirmed int    `json:"confirmed"`
	Rejected  int    `json:"rejected"`
	// Missed is spam a human or external filter moved into the spam folder
	// although Mailmune never flagged it ("Nicht erkannt").
	Missed int `json:"missed"`
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

// CalibrationBucket is one score band with the observed spam rate among
// human-reviewed decisions. A well-calibrated filter has MeanScore close to
// SpamRate.
type CalibrationBucket struct {
	Lower     float64 `json:"lower"`
	Upper     float64 `json:"upper"`
	Count     int     `json:"count"`
	MeanScore float64 `json:"meanScore"`
	SpamRate  float64 `json:"spamRate"`
}

// ThresholdMetric is precision/recall/FPR at one decision threshold, measured
// against human-confirmed labels within the reviewed candidate population.
type ThresholdMetric struct {
	Threshold float64 `json:"threshold"`
	TP        int     `json:"tp"`
	FP        int     `json:"fp"`
	TN        int     `json:"tn"`
	FN        int     `json:"fn"`
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	FPR       float64 `json:"fpr"`
}

// CalibrationReport summarizes how well the filter agrees with human reviews.
// It is computed only from confirmed/rejected decisions, so it reflects real
// local ground truth and never invented data.
type CalibrationReport struct {
	AccountID  string              `json:"accountId"`
	Reviewed   int                 `json:"reviewed"`
	Confirmed  int                 `json:"confirmed"`
	Rejected   int                 `json:"rejected"`
	Thresholds []ThresholdMetric   `json:"thresholds"`
	Buckets    []CalibrationBucket `json:"buckets"`
	// AutoMoveReady is true when precision at the safe auto-move threshold
	// meets the 99.5% target on a meaningful sample.
	AutoMoveReady bool `json:"autoMoveReady"`
}
