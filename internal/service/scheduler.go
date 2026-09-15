package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/RHM-GER/Mailmune/internal/domain"
	"github.com/RHM-GER/Mailmune/internal/events"
	"github.com/RHM-GER/Mailmune/internal/mailbox"
	"github.com/RHM-GER/Mailmune/internal/store"
)

// DefaultReconcileInterval is the standard period for the supplementary UID
// reconciliation. It guarantees that standby, network changes and dropped IDLE
// connections never lose messages: at worst a new message is seen one interval
// later.
const DefaultReconcileInterval = 10 * time.Minute

// Scheduler periodically reconciles every enabled account so new mail is
// detected without user interaction. Each cycle triggers an incremental scan;
// StartScan is idempotent and serialized per account, so an overlapping cycle
// or a still-running scan never causes a double scan. Connection failures are
// retried on the next tick, which is the bounded reconnect behavior.
type Scheduler struct {
	scanner  *Scanner
	store    *store.SQLite
	hub      *events.Hub
	interval time.Duration

	// stagger spreads accounts within a cycle so a large installation does not
	// open every IMAP connection at the same instant.
	stagger time.Duration

	mu       sync.Mutex
	cancel   context.CancelFunc
	loopCtx  context.Context
	watchers map[string]context.CancelFunc
	noIdle   map[string]bool
}

func newScheduler(scanner *Scanner, db *store.SQLite, hub *events.Hub, interval time.Duration) *Scheduler {
	if interval <= 0 {
		interval = DefaultReconcileInterval
	}
	// Keep the per-account stagger small relative to the interval.
	stagger := interval / 20
	if stagger > 5*time.Second {
		stagger = 5 * time.Second
	}
	return &Scheduler{scanner: scanner, store: db, hub: hub, interval: interval, stagger: stagger, watchers: map[string]context.CancelFunc{}, noIdle: map[string]bool{}}
}

// Start launches the periodic reconciliation loop. It runs one cycle shortly
// after startup and then every interval, until the returned stop function or
// the context ends. Starting twice is a no-op.
func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return
	}
	loopCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.loopCtx = loopCtx
	s.mu.Unlock()

	go s.loop(loopCtx)
}

// Stop ends the reconciliation loop. It is safe to call when not running.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *Scheduler) loop(ctx context.Context) {
	// A short initial delay lets the agent finish startup and the UI connect
	// before the first reconciliation opens IMAP connections.
	initial := time.NewTimer(s.initialDelay())
	defer initial.Stop()
	select {
	case <-ctx.Done():
		return
	case <-initial.C:
	}
	s.runCycle(ctx)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runCycle(ctx)
		}
	}
}

// initialDelay is small by default; tests override it to run a cycle at once.
func (s *Scheduler) initialDelay() time.Duration {
	if s.interval <= 2*time.Second {
		return 0
	}
	return 5 * time.Second
}

// runCycle triggers an incremental reconciliation for every enabled account
// and keeps the IDLE watchers in sync with the account list.
func (s *Scheduler) runCycle(ctx context.Context) {
	accounts, err := s.store.ListAccounts(ctx)
	if err != nil {
		s.publishError("", err)
		return
	}
	enabled := map[string]bool{}
	first := true
	for _, account := range accounts {
		if ctx.Err() != nil {
			return
		}
		if !account.Enabled {
			s.stopWatcher(account.ID)
			continue
		}
		enabled[account.ID] = true
		s.ensureWatcher(account.ID)
		if !first && s.stagger > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(s.stagger):
			}
		}
		first = false
		s.reconcileAccount(ctx, account)
	}
	s.mu.Lock()
	var stale []string
	for id := range s.watchers {
		if !enabled[id] {
			stale = append(stale, id)
		}
	}
	s.mu.Unlock()
	for _, id := range stale {
		s.stopWatcher(id)
	}
}

func (s *Scheduler) reconcileAccount(ctx context.Context, account domain.AccountConfig) {
	// Incremental reconciliation; resync=false keeps the persisted UID state.
	if _, err := s.scanner.StartScan(ctx, account.ID, false); err != nil {
		s.publishError(account.ID, err)
		return
	}
	if s.hub != nil {
		s.hub.Publish("schedule.reconcile", map[string]string{"accountId": account.ID})
	}
}

func (s *Scheduler) publishError(accountID string, err error) {
	if s.hub == nil {
		return
	}
	s.hub.Publish("schedule.error", map[string]string{"accountId": accountID, "error": redactError(err)})
}

// ensureWatcher starts an IDLE watcher for an account when none is running
// and the server was not already reported as lacking IDLE support. Watchers
// only exist while the scheduler loop is active (production), never when
// tests drive runCycle directly.
func (s *Scheduler) ensureWatcher(accountID string) {
	s.mu.Lock()
	if s.loopCtx == nil {
		s.mu.Unlock()
		return
	}
	if _, running := s.watchers[accountID]; running {
		s.mu.Unlock()
		return
	}
	if s.noIdle[accountID] {
		s.mu.Unlock()
		return
	}
	watchCtx, cancel := context.WithCancel(s.loopCtx)
	s.watchers[accountID] = cancel
	s.mu.Unlock()
	go s.watchAccount(watchCtx, accountID)
}

func (s *Scheduler) stopWatcher(accountID string) {
	s.mu.Lock()
	cancel := s.watchers[accountID]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// watchAccount holds an IDLE session on the account's inbox and triggers a
// debounced incremental scan whenever the server announces changes. If the
// server lacks IDLE, the account is marked and periodic reconciliation stays
// the only detection path.
func (s *Scheduler) watchAccount(ctx context.Context, accountID string) {
	defer func() {
		s.mu.Lock()
		delete(s.watchers, accountID)
		s.mu.Unlock()
	}()
	account, err := s.store.Account(ctx, accountID)
	if err != nil {
		return
	}
	password, err := s.scanner.secrets.Get(account.SecretRef)
	if err != nil {
		return
	}
	signals := make(chan struct{}, 1)
	notify := func() {
		select {
		case signals <- struct{}{}:
		default:
		}
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-signals:
				// Debounce: let a burst of arrivals settle, then drain extra
				// signals collected during the pause.
				select {
				case <-time.After(3 * time.Second):
				case <-ctx.Done():
					return
				}
				for drained := true; drained; {
					select {
					case <-signals:
					default:
						drained = false
					}
				}
				if _, err := s.scanner.StartScan(ctx, accountID, false); err != nil {
					s.publishError(accountID, err)
					continue
				}
				if s.hub != nil {
					s.hub.Publish("schedule.idle_scan", map[string]string{"accountId": accountID})
				}
			}
		}
	}()
	watchErr := s.scanner.mailbox.WatchFolder(ctx, account, password, account.InboxFolder, notify)
	if errors.Is(watchErr, mailbox.ErrIdleUnsupported) {
		s.mu.Lock()
		s.noIdle[accountID] = true
		s.mu.Unlock()
	}
}
