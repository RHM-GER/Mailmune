package mailbox

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/RHM-GER/Mailmune/internal/domain"
)

// ErrIdleUnsupported reports that a server offers neither IDLE nor IMAP4rev2.
// Callers fall back to the periodic UID reconciliation.
var ErrIdleUnsupported = errors.New("server does not support IMAP IDLE")

// watchSessionCap bounds a single IDLE connection. The library restarts IDLE
// automatically every 28 minutes; reconnecting periodically anyway keeps DNS,
// credentials and NAT state fresh and bounds silent half-open connections.
const watchSessionCap = 30 * time.Minute

const (
	watchBackoffInitial = 2 * time.Second
	watchBackoffMax     = 5 * time.Minute
)

func supportsIdleCaps(caps imap.CapSet) bool { return supportsIdle(caps) }

// WatchFolder keeps a read-only IDLE session on a folder and invokes onUpdate
// whenever the server announces mailbox changes (new mail). It reconnects with
// bounded exponential backoff plus jitter and returns only when ctx is
// cancelled or the server does not support IDLE at all. onUpdate runs on the
// watcher goroutine and must be cheap; the caller triggers the actual
// incremental scan on its own serialized connection.
func (m *Client) WatchFolder(ctx context.Context, account domain.AccountConfig, password, folder string, onUpdate func()) error {
	backoff := watchBackoffInitial
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := m.watchSession(ctx, account, password, folder, onUpdate)
		if errors.Is(err, ErrIdleUnsupported) {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil {
			// Clean session cap expiry: reconnect promptly.
			backoff = watchBackoffInitial
			continue
		}
		jitter := time.Duration(rand.Int63n(int64(backoff / 2)))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff + jitter):
		}
		backoff *= 2
		if backoff > watchBackoffMax {
			backoff = watchBackoffMax
		}
	}
}

// watchSession runs exactly one IDLE session. It returns nil after the
// session cap expires so the caller reconnects cleanly.
func (m *Client) watchSession(ctx context.Context, account domain.AccountConfig, password, folder string, onUpdate func()) error {
	handler := &imapclient.UnilateralDataHandler{
		Mailbox: func(*imapclient.UnilateralDataMailbox) {
			if onUpdate != nil {
				onUpdate()
			}
		},
		Expunge: func(uint32) {
			if onUpdate != nil {
				onUpdate()
			}
		},
	}
	client, err := m.connectWithHandler(ctx, account, password, handler)
	if err != nil {
		return err
	}
	defer client.Close()
	defer func() { _ = client.Logout().Wait() }()

	if !supportsIdleCaps(client.Caps()) {
		return ErrIdleUnsupported
	}
	if _, err := client.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
		return fmt.Errorf("select %s read-only: %w", folder, err)
	}
	idle, err := client.Idle()
	if err != nil {
		return fmt.Errorf("start idle: %w", err)
	}
	sessionCtx, cancelSession := context.WithTimeout(ctx, watchSessionCap)
	defer cancelSession()
	<-sessionCtx.Done()
	// Close sends DONE and unblocks the client for the logout.
	if err := idle.Close(); err != nil {
		return err
	}
	return ctx.Err()
}
