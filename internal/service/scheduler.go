package service

import (
	"context"
	"sync"
	"time"

	"github.com/RHM-GER/Mailmune/internal/domain"
	"github.com/RHM-GER/Mailmune/internal/events"
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

	mu     sync.Mutex
	cancel context.CancelFunc
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
	return &Scheduler{scanner: scanner, store: db, hub: hub, interval: interval, stagger: stagger}
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

// runCycle triggers an incremental reconciliation for every enabled account.
func (s *Scheduler) runCycle(ctx context.Context) {
	accounts, err := s.store.ListAccounts(ctx)
	if err != nil {
		s.publishError("", err)
		return
	}
	first := true
	for _, account := range accounts {
		if ctx.Err() != nil {
			return
		}
		if !account.Enabled {
			continue
		}
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
