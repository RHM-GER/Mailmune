package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
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

// Scanner serializes scan runs per account. Runs are crash-safe: progress
// lives in the database, synchronization resumes from the persisted UID
// state, and decisions are deduplicated by idempotency keys.
type Scanner struct {
	store   *store.SQLite
	secrets secrets.Store
	mailbox *mailbox.Client
	rules   *classifier.Rules
	ollama  *provider.Ollama
	hub     *events.Hub

	mu     sync.Mutex
	active map[string]*activeRun

	// testGate, when set, is invoked for every message before
	// classification. Tests use it to pause a run deterministically.
	testGate func()
}

type activeRun struct {
	runID  string
	cancel context.CancelFunc
	done   chan struct{}
}

// ScanEvent is the payload of scan.* events on the hub.
type ScanEvent struct {
	Run        domain.ScanRun `json:"run"`
	Candidates int            `json:"candidates,omitempty"`
	Moved      int            `json:"moved,omitempty"`
	// FolderMessages: exakte Nachrichtenanzahl des Ordners – die UI erklärt
	// damit begrenzte Erstläufe („neueste 1000 von 4321“) statt „etwa“.
	FolderMessages int      `json:"folderMessages,omitempty"`
	Warnings       []string `json:"warnings,omitempty"`
}

// minLearningSamples is the minimum number of confirmed examples before the
// statistical learner may contribute evidence. Small models stay silent so
// they cannot push cases over thresholds prematurely.
const minLearningSamples = 20

// debugScanAllMessages is a TEMPORARY testing aid. When true, every scanned
// message is stored as a decision, including ones below the candidate
// threshold, so the reviewer can inspect the score and evidence of messages
// that were NOT flagged and give feedback to improve detection. It persists
// harmless mail too, so it MUST be set back to false before release. It is a
// var (not const) so tests can exercise the production candidate-only path.
// TODO(revert): set to false again - tracked in TODO.md under "Testmodus".
var debugScanAllMessages = true

func newScanner(db *store.SQLite, secretStore secrets.Store, client *mailbox.Client, rules *classifier.Rules, ollama *provider.Ollama, hub *events.Hub) *Scanner {
	return &Scanner{store: db, secrets: secretStore, mailbox: client, rules: rules, ollama: ollama, hub: hub, active: map[string]*activeRun{}}
}

// RecoverInterrupted flags runs that were still active when the agent last
// stopped. Called once at startup; the next run resumes from the persisted
// UID state instead of blindly continuing.
func (s *Scanner) RecoverInterrupted(ctx context.Context) (int64, error) {
	return s.store.MarkInterruptedRuns(ctx)
}

// StartScan begins a background scan for an account. Starting a scan that is
// already running returns the active run unchanged (idempotent). When resync
// is true, the stored UID state of the inbox folder is dropped first, so the
// whole mailbox is re-read (decisions stay deduplicated by idempotency keys).
func (s *Scanner) StartScan(ctx context.Context, accountID string, resync bool) (domain.ScanRun, error) {
	return s.startScan(ctx, accountID, resync, false, time.Time{})
}

// StartScanSince ist ein Komplett-Rescan, der auf Nachrichten ab dem
// angegebenen Empfangsdatum begrenzt ist (zero = alles). Die UI nutzt das für
// „Neu prüfen“ mit benutzerdefiniertem Bereich.
func (s *Scanner) StartScanSince(ctx context.Context, accountID string, since time.Time) (domain.ScanRun, error) {
	return s.startScan(ctx, accountID, true, false, since)
}

// StartDeepScan re-reads every message received since the account's last deep
// scan (the last 7 days when none ran yet) and reviews all of them with the
// validated local model, like an explicit full rescan does. Missed weekly
// appointments catch up exactly once: the window always reaches back to the
// last successful deep scan. Triggered by the weekly schedule or manually.
func (s *Scanner) StartDeepScan(ctx context.Context, accountID string) (domain.ScanRun, error) {
	return s.startScan(ctx, accountID, true, true, time.Time{})
}

func (s *Scanner) startScan(ctx context.Context, accountID string, resync, deep bool, since time.Time) (domain.ScanRun, error) {
	account, err := s.store.Account(ctx, accountID)
	if err != nil {
		return domain.ScanRun{}, fmt.Errorf("account not found: %w", err)
	}
	password, err := s.secrets.Get(account.SecretRef)
	if err != nil {
		if errors.Is(err, secrets.ErrNotFound) {
			return domain.ScanRun{}, errors.New("stored password is missing; save the account again")
		}
		return domain.ScanRun{}, err
	}
	if resync {
		if err := s.store.DeleteFolderSyncState(ctx, accountID, account.InboxFolder); err != nil {
			return domain.ScanRun{}, err
		}
		// Re-check the spam-folder sweep from scratch too; the missed_log
		// deduplication keeps the counters correct across a full re-read.
		if account.SpamFolder != "" && !strings.EqualFold(account.SpamFolder, account.InboxFolder) {
			if err := s.store.DeleteFolderSyncState(ctx, accountID, account.SpamFolder); err != nil {
				return domain.ScanRun{}, err
			}
		}
	}

	s.mu.Lock()
	if entry, ok := s.active[accountID]; ok {
		s.mu.Unlock()
		run, found, err := s.store.ScanRun(ctx, entry.runID)
		if err != nil || !found {
			return domain.ScanRun{}, err
		}
		return run, nil
	}
	now := time.Now().UTC()
	run := domain.ScanRun{ID: uuid.NewString(), AccountID: accountID, Status: domain.ScanRunning, Folder: account.InboxFolder, StartedAt: now, UpdatedAt: now}
	if err := s.store.CreateScanRun(ctx, run); err != nil {
		s.mu.Unlock()
		return domain.ScanRun{}, err
	}
	runCtx, cancel := context.WithCancel(context.Background())
	entry := &activeRun{runID: run.ID, cancel: cancel, done: make(chan struct{})}
	s.active[accountID] = entry
	s.mu.Unlock()

	s.publish("scan.started", ScanEvent{Run: run})
	go s.execute(runCtx, entry.done, account, password, run, resync, deep, since)
	return run, nil
}

// CancelScan requests cancellation of the account's active run.
func (s *Scanner) CancelScan(ctx context.Context, accountID string) (domain.ScanRun, bool, error) {
	s.mu.Lock()
	entry, ok := s.active[accountID]
	s.mu.Unlock()
	if !ok {
		return domain.ScanRun{}, false, nil
	}
	entry.cancel()
	run, found, err := s.store.ScanRun(ctx, entry.runID)
	if err != nil || !found {
		return domain.ScanRun{}, false, err
	}
	return run, true, nil
}

// Wait blocks until the account's active run has finished. It returns false
// when no run is active. Intended for tests.
func (s *Scanner) Wait(accountID string, timeout time.Duration) bool {
	s.mu.Lock()
	entry, ok := s.active[accountID]
	s.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case <-entry.done:
		return true
	case <-time.After(timeout):
		return false
	}
}

func (s *Scanner) execute(ctx context.Context, done chan struct{}, account domain.AccountConfig, password string, run domain.ScanRun, aiAll bool, deep bool, since time.Time) {
	defer close(done)
	defer func() {
		s.mu.Lock()
		delete(s.active, account.ID)
		s.mu.Unlock()
	}()

	result := s.scanAccount(ctx, account, password, run, aiAll, deep, since)

	runRow, _, err := s.store.ScanRun(context.Background(), run.ID)
	if err != nil {
		runRow = run
	}
	s.publish("scan.finished", ScanEvent{Run: runRow, Candidates: result.candidates, Moved: result.moved, FolderMessages: result.folderMessages, Warnings: result.warnings})
}

type scanResult struct {
	processed      int
	candidates     int
	moved          int
	folderMessages int
	warnings       []string
}

// baselineVirtualMessages bounds how strongly an imported global baseline may
// influence scoring, regardless of its raw size. It is expressed as a virtual
// count of confirmed examples so a huge corpus never drowns out the user's own
// confirmed learning.
const baselineVirtualMessages = 150

// buildScorer merges the per-account learner with the optional global
// baseline. Either part may be absent; nil is returned when there is nothing
// to score with.
func (s *Scanner) buildScorer(ctx context.Context, accountID string) *learning.Model {
	accountModel, accountErr := s.store.LoadLearningModel(ctx, accountID)
	if accountErr != nil {
		accountModel = nil
	}
	baselineModel, _, baselineOK, baselineErr := s.store.LoadBaseline(ctx, "global")
	if baselineErr != nil || !baselineOK {
		baselineModel = nil
	}
	switch {
	case accountModel != nil && baselineModel != nil:
		return accountModel.Merged(baselineModel, baselineModel.VirtualWeight(baselineVirtualMessages))
	case accountModel != nil:
		return accountModel
	case baselineModel != nil:
		// A baseline alone is a prior; expose it at its bounded virtual size.
		return learning.NewModel().Merged(baselineModel, baselineModel.VirtualWeight(baselineVirtualMessages))
	default:
		return nil
	}
}

func (s *Scanner) scanAccount(ctx context.Context, account domain.AccountConfig, password string, run domain.ScanRun, aiAll bool, deep bool, since time.Time) scanResult {
	result := scanResult{}
	folder := account.InboxFolder

	prev, _, err := s.store.FolderSyncState(ctx, account.ID, folder)
	if err != nil {
		s.finish(run.ID, domain.ScanFailed, redactError(err))
		return result
	}
	// A full re-read (explicit resync or the very first scan of a folder)
	// reviews EVERY message with the model; incremental runs only consult it
	// for the ambiguous band so live detection of new mail stays fast.
	aiAll = aiAll || (prev.UIDValidity == 0 && prev.LastUID == 0)

	// A deep scan is date-bounded: it re-reads everything since the last deep
	// scan (default window: 7 days) instead of the whole history.
	var deepSince time.Time
	if deep {
		deepSince = time.Now().UTC().AddDate(0, 0, -7)
		if account.LastDeepScanAt != nil && account.LastDeepScanAt.After(deepSince) {
			deepSince = *account.LastDeepScanAt
		}
	}
	// Benutzerdefinierter Bereich aus der UI überschreibt das Tiefscan-Fenster.
	if !since.IsZero() {
		deepSince = since
	}

	progressCounter := 0
	opts := mailbox.SyncOptions{
		MaxMessages: mailbox.DefaultMaxMessages,
		Since:       deepSince,
		FetchText:   true,
		OnProgress: func(processed, estimatedTotal, folderMessages int) {
			progressCounter++
			// Persist every step so crashes lose at most one message of
			// progress; publish slightly less often to keep the stream calm.
			_ = s.store.UpdateScanProgress(context.Background(), run.ID, processed, estimatedTotal)
			if progressCounter%5 == 0 || processed == estimatedTotal {
				if current, ok, _ := s.store.ScanRun(context.Background(), run.ID); ok {
					s.publish("scan.progress", ScanEvent{Run: current, FolderMessages: folderMessages})
				}
			}
		},
	}
	// Note: the bounded 90-day first pass belongs to the profiling feature
	// (Phase 4). Regular scans read up to MaxMessages newest messages.

	// Load the statistical learner once per run: the per-account model from
	// confirmed reviews, merged with the optional global baseline imported
	// from an external corpus. The baseline acts as a bounded prior so the
	// user's own confirmed learning can still outweigh it. A missing or
	// broken model never blocks scanning.
	var scorer classifier.StatisticalScorer
	// KI-kompilierte Profil-Indikatoren: nur aktiv, wenn sie eingeschaltet
	// sind UND zum aktuellen Profiltext passen – ein veraltetes Kompilat wird
	// nie still weiterverwendet (die UI zeigt „veraltet“ und bietet die
	// Neugenerierung an).
	var profileIndicators *domain.ProfileIndicators
	var profilePrompt string
	// Embedding-Fast-Pfad: Zentroide nur laden, wenn ein Embedding-Modell
	// gewählt ist UND importierte Zentroide für genau dieses Modell liegen
	// (Vektoren sind modellspezifisch).
	var embedCentroids *domain.EmbeddingCentroids
	if account.EmbeddingModel != "" {
		if centroids, ok, err := s.store.LoadEmbeddingCentroids(ctx, account.EmbeddingModel); err == nil && ok {
			embedCentroids = &centroids
		} else {
			log.Printf("scan %s: Embedding-Modell %s gewählt, aber keine Zentroide importiert (mltool embed) – Fast-Pfad inaktiv", account.ID, account.EmbeddingModel)
		}
	}
	profileStale := false
	if compiled, found, loadErr := s.store.GetProfileModel(ctx, account.ID); loadErr == nil && found && compiled.Enabled {
		if compiled.SourceHash == ProfileSourceHash(account.Profile) {
			profileIndicators = &compiled.Indicators
			profilePrompt = compiled.Prompt
			log.Printf("scan %s: KI-Profilmodell aktiv, Indikatoren + Prompt im Einsatz", account.ID)
		} else {
			profileStale = true
			log.Printf("scan %s: KI-Profilmodell VERALTET (Profiltext geändert) – Indikatoren/Prompt pausiert, Rekompilierung nach dem Lauf", account.ID)
		}
	} else {
		log.Printf("scan %s: kein aktives KI-Profilmodell – nur eingebauten Regeln/Vertikalen", account.ID)
	}
	// learned is the bounded per-profile context for the optional local model
	// ("RAG light"): the same confirmed knowledge that drives scoring, built
	// once per run. It stays nil until the profile has enough confirmed
	// examples, so an untrained account sends the plain contract.
	var learned *provider.LearnedContext
	if combined := s.buildScorer(ctx, account.ID); combined != nil && combined.Trained() >= minLearningSamples {
		scorer = combined
		if spamSignals, hamSignals := combined.TopSignals(8); len(spamSignals)+len(hamSignals) > 0 {
			learned = &provider.LearnedContext{SpamSignals: spamSignals, HamSignals: hamSignals}
		}
	}
	// Der kompilierte Profil-Prompt wandert zusätzlich in den Modell-Kontext,
	// damit die KI mit demselben Profilverständnis urteilt wie die Regeln.
	if profilePrompt != "" {
		if learned == nil {
			learned = &provider.LearnedContext{}
		}
		learned.ProfilePrompt = profilePrompt
	}

	// modelStats zählt die KI-Consults dieses Laufs (Erfolge, Fehlerarten,
	// Profil-Prompt-Nutzung) für die Debug-Logs und die Lauf-Warnungen.
	stats := &modelRunStats{}

	handler := func(message domain.MessageFeatures, text string) error {
		if s.testGate != nil {
			s.testGate()
		}
		// Text-based rules need the bounded body text; it is discarded after
		// classification and never persisted.
		message.Text = text
		if message.URLCount == 0 {
			message.URLCount = classifier.CountURLs(text)
		}
		hash := sha256.Sum256([]byte(strings.ToLower(message.MessageID)))
		hashHex := hex.EncodeToString(hash[:])
		features := learning.ExtractFeatures(message.Subject, message.From, message.FromDomain, text)
		classification := s.rules.ClassifyFull(message, account.Profile, profileIndicators, features, scorer)
		// Embedding-Fast-Pfad (System 1): Vektor der Mail gegen die Spam-/Ham-
		// Zentroide – eine eigene Signalgruppe, ganz ohne Textgenerierung.
		if embedCentroids != nil {
			embedText := message.Subject + "\n" + message.From + "\n" + message.Text
			if len(embedText) > 8000 {
				embedText = embedText[:8000]
			}
			if vec, err := s.ollama.Embed(ctx, account.EmbeddingModel, []string{embedText}); err == nil && len(vec) == 1 && len(vec[0]) == embedCentroids.Dim {
				margin := classifier.Cosine(vec[0], embedCentroids.SpamCenter) - classifier.Cosine(vec[0], embedCentroids.HamCenter)
				classifier.ApplyEmbeddingMargin(&classification, margin)
				log.Printf("debug scan %s mail=%s: embedding margin=%+.3f", account.ID, hashHex[:8], margin)
			} else if err != nil {
				log.Printf("debug scan %s mail=%s: embedding fehlgeschlagen: %v", account.ID, hashHex[:8], err)
			}
		}
		for _, ev := range classification.Evidence {
			if ev.Code == classifier.CodeProfileOffTopic || ev.Code == classifier.CodeProfileTopicMatch {
				log.Printf("debug scan %s mail=%s: Profil-Indikator %s (%s)", account.ID, hashHex[:8], ev.Code, ev.Summary)
			}
		}
		s.consultModel(ctx, account, message, &classification, learned, aiAll, stats)
		action := classifier.Decide(account.SafetyMode, classification)
		isCandidate := action != classifier.ActionIgnore
		// Count every arrival for the dashboard's "Eingang" series, including
		// messages below the threshold that are not persisted as decisions.
		// Only day + message-ID hash are stored; the log deduplicates rescans.
		_, _ = s.store.LogReceived(context.Background(), account.ID, hashHex, message.ReceivedAt)
		// Testing mode (debugScanAllMessages): also store ignored messages so the
		// reviewer can see why they were not flagged. In normal operation only
		// candidates reach the review list and everything below the threshold is
		// skipped without being persisted.
		if !isCandidate && !debugScanAllMessages {
			return nil
		}
		if isCandidate {
			result.candidates++
		}
		status := domain.StatusPending
		current := message.Folder
		if action == classifier.ActionMove && !account.DryRun {
			destinationUID, moveErr := s.mailbox.MoveAtomic(ctx, account, password, message.Folder, message.UID, account.SpamFolder)
			if moveErr != nil {
				result.warnings = append(result.warnings, fmt.Sprintf("UID %d nicht verschoben: %v", message.UID, redactError(moveErr)))
			} else {
				status = domain.StatusMoved
				current = account.SpamFolder
				result.moved++
				if destinationUID != 0 {
					message.UID = destinationUID
				}
			}
		}
		decision := domain.MessageDecision{
			ID: uuid.NewString(), AccountID: account.ID, UIDValidity: message.UIDValidity, UID: message.UID,
			MessageIDHash: hashHex, OriginFolder: message.Folder, CurrentFolder: current,
			From: message.From, Subject: message.Subject, Score: classification.Score, Status: status,
			Evidence: classification.Evidence, ModelVersion: classification.ModelUsed,
			IdempotencyKey: fmt.Sprintf("scan:%s:%d:%d:%s", account.ID, message.UIDValidity, message.UID, message.Folder),
			ReceivedAt: message.ReceivedAt, CreatedAt: time.Now().UTC(),
		}
		// SaveDecision deduplicates on (account, validity, uid, folder); the
		// returned ID is the stored row's ID, which may differ from the freshly
		// generated one after a resync. Features must attach to the stored ID.
		storedID, created, err := s.store.SaveDecision(context.Background(), decision)
		if err != nil {
			return err
		}
		if !created {
			// Resync of an already-stored message: refresh the mutable
			// classification of a still-pending decision so an improved rule
			// set, learning model or validated local LLM verdict is reflected
			// instead of being discarded by the dedup. Reviewed and moved
			// decisions are ground truth and stay frozen.
			if _, err := s.store.RefreshPendingDecision(context.Background(), storedID, classification.Score, classification.Evidence, classification.ModelUsed, decision.Subject); err != nil {
				return err
			}
		}
		// Keep the compact feature vector so a confirmed review can train the
		// local model later. It is deleted after training or purged after 180
		// days; it never contains raw message text.
		if err := s.store.SaveDecisionFeatures(context.Background(), storedID, features); err != nil {
			return err
		}
		result.processed++
		return nil
	}

	outcome, syncErr := s.mailbox.SyncFolder(ctx, account, password, folder, stateOrNil(prev), opts, handler)
	if outcome != nil {
		result.folderMessages = outcome.FolderMessages
	}

	// Persist sync state only when the run completed or was cancelled after
	// processing; on unexpected errors the unchanged state forces a retry,
	// and decision idempotency keys keep the retry duplicate-free.
	if outcome != nil && outcome.UIDValidity != 0 && (syncErr == nil || errors.Is(syncErr, context.Canceled)) {
		state := domain.FolderSyncState{AccountID: account.ID, Folder: folder, UIDValidity: outcome.UIDValidity, LastUID: outcome.LastUID, LastSyncAt: time.Now().UTC()}
		if err := s.store.SaveFolderSyncState(context.Background(), state); err != nil {
			s.finish(run.ID, domain.ScanFailed, redactError(err))
			return result
		}
	}

	switch {
	case syncErr == nil:
		account.LastScanAt = ptrTime(time.Now().UTC())
		if deep {
			// Bound the next deep scan window; a failed deep scan leaves the old
			// value untouched so the missed window is caught up on the retry.
			account.LastDeepScanAt = ptrTime(time.Now().UTC())
		}
		account.UpdatedAt = time.Now().UTC()
		_ = s.store.UpsertAccount(context.Background(), account)
		// Feed the dashboard statistics: everything scanned today plus any
		// automatic moves performed during this run.
		scanned := 0
		if outcome != nil {
			scanned = outcome.Processed
		}
		_ = s.store.RecordDailyStats(context.Background(), account.ID, "", scanned, result.moved, 0, 0)
		// Arrival counters are pure metadata; keep them bounded anyway.
		_, _ = s.store.PurgeReceivedLog(context.Background())
		// KI konfiguriert, aber nicht erreichbar: einmalig pro Lauf warnen,
		// damit klar ist, dass die KI-Filterung ausgefallen ist.
		log.Printf("scan %s: KI-Zusammenfassung consults=%d ok=%d nicht_erreichbar=%d andere_fehler=%d profilPrompt=%t indikatoren=%t", account.ID, stats.consulted, stats.ok, stats.unreachable, stats.other, stats.profilePrompt, profileIndicators != nil)
		if s.hub != nil {
			switch {
			case stats.ok == 0 && stats.unreachable > 0:
				s.hub.Publish("model.unavailable", map[string]string{"accountId": account.ID, "model": account.OllamaModel, "error": redactError(stats.firstErr)})
			case stats.other > 0:
				// Einzelfehler (Timeout, Kontext, Modell-404 …) sind KEIN
				// „Ollama läuft nicht“ – echte Ursache kommunizieren.
				s.hub.Publish("model.error", map[string]string{"accountId": account.ID, "model": account.OllamaModel, "error": redactError(stats.firstErr), "summary": fmt.Sprintf("%d von %d KI-Consults fehlgeschlagen", stats.other, stats.consulted)})
			}
		}
		// Veraltetes Kompilat? Nach dem Lauf automatisch neu kompilieren, damit
		// Profiländerungen nie still ungenutzt bleiben.
		if profileStale {
			go func() {
				if _, err := s.recompileProfile(context.Background(), account); err != nil {
					log.Printf("profile recompile %s: %v", account.ID, err)
					return
				}
				if s.hub != nil {
					s.hub.Publish("profile.compiled", map[string]string{"accountId": account.ID, "reason": "auto"})
				}
			}()
		}
		// Spam, das ein Mensch oder ein Fremdfilter einsortiert hat, wird als
		// "Nicht erkannt"-Serie erfasst (read-only Sweep des Spam-Ordners).
		s.sweepSpamFolder(ctx, account, password, run)
		s.finish(run.ID, domain.ScanCompleted, "")
	case errors.Is(syncErr, context.Canceled):
		s.finish(run.ID, domain.ScanCancelled, "")
	default:
		s.finish(run.ID, domain.ScanFailed, redactError(syncErr))
	}
	return result
}

// sweepSpamFolder counts MISSED spam: messages sitting in the account's spam
// folder that Mailmune itself never flagged - a human moved them there by hand
// in a mail client, or an external filter did. It feeds the dashboard's
// "Nicht erkannt" series (measured against the whole arrival total). The
// sweep is strictly read-only: it never moves or modifies anything and stores
// only day + message-ID hash. A missing/unreadable spam folder is a warning,
// never a failed run.
func (s *Scanner) sweepSpamFolder(ctx context.Context, account domain.AccountConfig, password string, run domain.ScanRun) int {
	folder := account.SpamFolder
	if folder == "" || strings.EqualFold(folder, account.InboxFolder) {
		return 0
	}
	prev, _, err := s.store.FolderSyncState(ctx, account.ID, folder)
	if err != nil {
		return 0
	}
	missed := 0
	opts := mailbox.SyncOptions{MaxMessages: mailbox.DefaultMaxMessages}
	handler := func(message domain.MessageFeatures, _ string) error {
		if strings.TrimSpace(message.MessageID) == "" {
			// Without a message ID there is no dedup key; skip to avoid
			// double-counting on rescans.
			return nil
		}
		hash := sha256.Sum256([]byte(strings.ToLower(message.MessageID)))
		hashHex := hex.EncodeToString(hash[:])
		if flagged, err := s.store.FlaggedByMessageIDHash(ctx, account.ID, hashHex); err != nil || flagged {
			return nil
		}
		if first, err := s.store.LogMissed(ctx, account.ID, hashHex, message.ReceivedAt); err == nil && first {
			missed++
		}
		return nil
	}
	outcome, syncErr := s.mailbox.SyncFolder(ctx, account, password, folder, stateOrNil(prev), opts, handler)
	if outcome != nil && outcome.UIDValidity != 0 && (syncErr == nil || errors.Is(syncErr, context.Canceled)) {
		_ = s.store.SaveFolderSyncState(context.Background(), domain.FolderSyncState{
			AccountID: account.ID, Folder: folder,
			UIDValidity: outcome.UIDValidity, LastUID: outcome.LastUID, LastSyncAt: time.Now().UTC(),
		})
	}
	if syncErr != nil && !errors.Is(syncErr, context.Canceled) {
		s.publish("scan.progress", ScanEvent{Run: run, Warnings: []string{
			"\"Nicht erkannt\"-Pr\u00fcung: Spam-Ordner nicht lesbar (" + redactError(syncErr) + ")",
		}})
	}
	return missed
}

// recompileProfile erzeugt das KI-Profilmodell neu (Prompt + Indikatoren) –
// die einzige Implementierung; der Service-Endpoint und die automatische
// Rekompilierung nach veraltetem Kompilat nutzen beide diesen Weg.
func (s *Scanner) recompileProfile(ctx context.Context, account domain.AccountConfig) (domain.ProfileModel, error) {
	if !account.OllamaValidated || account.OllamaModel == "" {
		return domain.ProfileModel{}, errors.New("für die Profil-Kompilierung ist ein validiertes lokales KI-Modell erforderlich")
	}
	if strings.TrimSpace(account.Profile.Purpose) == "" && strings.TrimSpace(account.Profile.Context) == "" && strings.TrimSpace(account.Profile.Industry) == "" && strings.TrimSpace(account.Profile.Unexpected) == "" {
		return domain.ProfileModel{}, errors.New("das Profil ist zu leer für die Kompilierung")
	}
	compiled, err := s.ollama.CompileProfile(ctx, account.OllamaModel, account.Profile)
	if err != nil {
		return domain.ProfileModel{}, err
	}
	model := domain.ProfileModel{
		AccountID:  account.ID,
		SourceHash: ProfileSourceHash(account.Profile),
		CompiledAt: time.Now().UTC(),
		Model:      account.OllamaModel,
		Prompt:     compiled.Prompt,
		Indicators: compiled.Indicators,
		Enabled:    true,
	}
	if err := s.store.SaveProfileModel(ctx, model); err != nil {
		return domain.ProfileModel{}, err
	}
	return model, nil
}

// modelRunStats zählt die KI-Consults eines Laufs und merkt sich, ob der
// kompilierte Profil-Prompt mitgeschickt wurde – die Basis für die
// Debug-Logs und für die Unterscheidung „Ollama down“ vs. Einzelfehler.
type modelRunStats struct {
	consulted     int
	ok            int
	unreachable   int
	other         int
	firstErr      error
	profilePrompt bool
}

// mailDebugID liefert einen kurzen stabilen Bezeichner für Logs (erste 8 Hex-
// Zeichen des Message-ID-Hashes) – im Log lesbar, ohne Adressen im Klartext
// zu schreiben.
func mailDebugID(messageID string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(messageID))))
	return hex.EncodeToString(sum[:4])
}

// consultModel runs the optional local Ollama classification and merges a
// validated verdict as a single independent signal group. On an explicit full
// scan (aiAll) it reviews every message that is not already near-certain spam,
// so low-scoring mail the rules missed still gets a second opinion; on
// incremental/new-mail scans only the ambiguous band is sent, keeping live
// detection fast and the machine free.
func (s *Scanner) consultModel(ctx context.Context, account domain.AccountConfig, message domain.MessageFeatures, classification *domain.Classification, learned *provider.LearnedContext, aiAll bool, stats *modelRunStats) bool {
	if !account.AIEnabled || !account.OllamaValidated || account.OllamaModel == "" {
		return false
	}
	if classification.Score >= 0.98 {
		return false
	}
	if !aiAll && classification.Score < 0.25 {
		return false
	}
	stats.consulted++
	hasPrompt := learned != nil && strings.TrimSpace(learned.ProfilePrompt) != ""
	if hasPrompt {
		stats.profilePrompt = true
	}
	verdict, err := s.ollama.Classify(ctx, account.OllamaModel, message, account.Profile, learned)
	if err != nil {
		if stats.firstErr == nil {
			stats.firstErr = err
		}
		if errors.Is(err, provider.ErrUnreachable) {
			stats.unreachable++
		} else {
			stats.other++
		}
		log.Printf("debug scan %s mail=%s: KI-Consult FEHLER: %v", account.ID, mailDebugID(message.MessageID), err)
		return false
	}
	stats.ok++
	log.Printf("debug scan %s mail=%s: KI-Consult ok model=%s profilPrompt=%t verdict=%s score=%.2f", account.ID, mailDebugID(message.MessageID), account.OllamaModel, hasPrompt, verdict.Class, verdict.Score)
	classification.ModelUsed = account.OllamaModel
	classification.ModelValidated = true
	classification.Evidence = append(classification.Evidence, domain.Evidence{Group: "model", Code: "local_model_" + verdict.Class, Weight: verdict.Score, Summary: "Lokales validiertes Modell: " + verdict.Class})
	if verdict.Class == "spam" {
		// A spam verdict may only LIFT the score, never lower it: small local
		// models often emit poorly calibrated confidence values (even "spam"
		// with a tiny score), and the rules' score already encodes the
		// deterministic evidence. Decide() still needs >= 2 independent groups
		// to auto-move, so the LLM alone never moves mail.
		//
		// A strong trust signal (authenticated canonical brand with aligned
		// envelope, explicit allow-list, known correspondent) blocks the lift
		// ENTIRELY: the model is known to misjudge genuine brand mail, and a
		// blend would drag a ~3% rules score up to ~50% on a confident spam
		// verdict. The evidence entry and the extra group are kept for
		// transparency; the score stays what the deterministic rules say.
		if !classification.StrongTrustSignal {
			switch {
			case verdict.Score >= 0.7:
				// Confident verdict: at least into the review list.
				blended := classification.Score*0.5 + verdict.Score*0.5
				if floor := classifier.CandidateThreshold + 0.05; blended < floor {
					blended = floor
				}
				if blended > classification.Score {
					classification.Score = blended
				}
			case verdict.Score >= 0.5:
				if blended := classification.Score*0.75 + verdict.Score*0.25; blended > classification.Score {
					classification.Score = blended
				}
			}
			// Low-confidence spam keeps the rules' score; the evidence entry and
			// the extra independent group still count.
		}
		classification.IndependentGroups++
	}
	// A strong trust signal caps the score below the review threshold: genuine
	// authenticated brand mail or explicitly trusted senders must not end up as
	// spam candidates just because the model or a weak heuristic disagrees.
	// Explicit deny rules still win over implicit trust.
	if classification.StrongTrustSignal && classification.Score >= classifier.CandidateThreshold && !hasDenyEvidence(classification.Evidence) {
		classification.Score = classifier.CandidateThreshold - 0.01
	}
	return true
}

func hasDenyEvidence(evidence []domain.Evidence) bool {
	for _, item := range evidence {
		if item.Group == "rules" && item.Weight > 0 {
			return true
		}
	}
	return false
}

func (s *Scanner) finish(runID string, status domain.ScanStatus, errMsg string) {
	_ = s.store.FinishScanRun(context.Background(), runID, status, errMsg)
}

func (s *Scanner) publish(typ string, event ScanEvent) {
	if s.hub == nil {
		return
	}
	s.hub.Publish(typ, event)
}

// publishMove announces a completed move/restore so the UI can update the
// decision's current folder without a full refresh.
func (s *Scanner) publishMove(accountID string, state domain.MoveState) {
	if s.hub == nil {
		return
	}
	s.hub.Publish("move.completed", map[string]string{"accountId": accountID, "state": string(state)})
}

func stateOrNil(state domain.FolderSyncState) *domain.FolderSyncState {
	if state.UIDValidity == 0 && state.LastUID == 0 {
		return nil
	}
	return &state
}

// redactError keeps error text short and free of obvious secrets before it
// reaches the database, events or API responses.
func redactError(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	for _, marker := range []string{"password=", "password:", "Authorization"} {
		if index := strings.Index(strings.ToLower(text), strings.ToLower(marker)); index >= 0 {
			text = text[:index] + "[entfernt]"
		}
	}
	if len(text) > 400 {
		text = text[:400]
	}
	return text
}

func ptrTime(value time.Time) *time.Time { return &value }
