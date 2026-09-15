package mailbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"

	"github.com/RHM-GER/Mailmune/internal/mailbox/imaptest"
)

func TestWatchFolderTriggersOnNewMessage(t *testing.T) {
	server := imaptest.New(t, moveCaps())
	client := newTestClient(server)
	account := testAccount(server)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := make(chan struct{}, 16)
	done := make(chan error, 1)
	go func() {
		done <- client.WatchFolder(ctx, account, imaptest.Password, "INBOX", func() {
			select {
			case updates <- struct{}{}:
			default:
			}
		})
	}()

	// The watch session needs a moment to connect, select and enter IDLE.
	// Add messages in a retry loop so the test does not depend on timing.
	received := false
	for attempt := 0; attempt < 10 && !received; attempt++ {
		server.AddMessage("INBOX", "neu@example.com", "Neu", "neu", time.Now())
		select {
		case <-updates:
			received = true
		case <-time.After(500 * time.Millisecond):
		}
	}
	if !received {
		t.Fatal("IDLE watcher never reported the new message")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("watch returned unexpected error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("WatchFolder did not return after cancel")
	}
}

func TestSupportsIdleDecision(t *testing.T) {
	// Production guard: IDLE is only attempted when the server advertises it
	// (rev1) or speaks IMAP4rev2 (IDLE is core). The imapserver test backend
	// always offers IDLE for rev1, so this decision is unit-tested here.
	if !supportsIdle(imap.CapSet{imap.CapIMAP4rev2: {}}) {
		t.Fatal("IMAP4rev2 must imply IDLE")
	}
	if !supportsIdle(imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapIdle: {}}) {
		t.Fatal("advertised IDLE must be accepted")
	}
	if supportsIdle(imap.CapSet{imap.CapIMAP4rev1: {}}) {
		t.Fatal("rev1 without IDLE must be refused")
	}
	if supportsMove(imap.CapSet{imap.CapIMAP4rev1: {}}) {
		t.Fatal("rev1 without MOVE must be refused")
	}
}

func TestWatchFolderStopsOnCancelWithoutMail(t *testing.T) {
	server := imaptest.New(t, moveCaps())
	client := newTestClient(server)
	account := testAccount(server)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- client.WatchFolder(ctx, account, imaptest.Password, "INBOX", func() {})
	}()
	// Let the session establish, then cancel: the watcher must return
	// promptly without any mailbox change.
	time.Sleep(300 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("unexpected watch error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("WatchFolder did not return after cancel")
	}
}
