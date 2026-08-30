package mailbox

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"

	"github.com/RHM-GER/Mailmune/internal/domain"
	"github.com/RHM-GER/Mailmune/internal/mailbox/imaptest"
)

func moveCaps() imap.CapSet {
	return imap.CapSet{imap.CapIMAP4rev2: {}, imap.CapMove: {}, imap.CapIdle: {}}
}

// noMoveCaps simulates an IMAP4rev1 server without the MOVE extension.
// (IMAP4rev2 always includes MOVE, so rev1 is the only way to test refusal.)
func noMoveCaps() imap.CapSet {
	return imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapIdle: {}}
}

func testAccount(server *imaptest.Server) domain.AccountConfig {
	host, portText, err := net.SplitHostPort(server.Addr)
	if err != nil {
		panic(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		panic(err)
	}
	return domain.AccountConfig{ID: "acc", Host: host, Port: port, Username: imaptest.Username, InboxFolder: "INBOX", SentFolder: "Sent", SpamFolder: "AI_SPAM_FILTER"}
}

func newTestClient(server *imaptest.Server) *Client {
	return NewClientWithRoots(server.Roots, 5*time.Second)
}

func syncState(outcome *SyncOutcome) *domain.FolderSyncState {
	return &domain.FolderSyncState{AccountID: "acc", Folder: "INBOX", UIDValidity: outcome.UIDValidity, LastUID: outcome.LastUID, LastSyncAt: time.Now().UTC()}
}

func collectSync(t *testing.T, client *Client, account domain.AccountConfig, prev *domain.FolderSyncState, opts SyncOptions) (*SyncOutcome, []domain.MessageFeatures) {
	t.Helper()
	var seen []domain.MessageFeatures
	outcome, err := client.SyncFolder(context.Background(), account, imaptest.Password, "INBOX", prev, opts, func(features domain.MessageFeatures, _ string) error {
		seen = append(seen, features)
		return nil
	})
	if err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	return outcome, seen
}

func TestInitialSyncIsBoundedAndUIDBased(t *testing.T) {
	server := imaptest.New(t, moveCaps())
	now := time.Now()
	for i := 0; i < 5; i++ {
		server.AddMessage("INBOX", "sender@example.com", "Nachricht "+strconv.Itoa(i), "Inhalt "+strconv.Itoa(i), now.Add(-time.Duration(i)*time.Hour))
	}
	client := newTestClient(server)
	account := testAccount(server)

	outcome, seen := collectSync(t, client, account, nil, SyncOptions{MaxMessages: 1000})
	if !outcome.Resync {
		t.Fatal("first sync must be marked as resync")
	}
	if len(seen) != 5 || outcome.Processed != 5 {
		t.Fatalf("expected 5 messages, saw %d (processed %d)", len(seen), outcome.Processed)
	}
	if outcome.LastUID != 5 {
		t.Fatalf("last UID = %d, want 5", outcome.LastUID)
	}
	for _, features := range seen {
		if features.UID == 0 || features.UIDValidity == 0 || features.From == "" {
			t.Fatalf("incomplete features: %+v", features)
		}
		if features.UIDValidity != outcome.UIDValidity {
			t.Fatalf("UIDVALIDITY mismatch: %d vs %d", features.UIDValidity, outcome.UIDValidity)
		}
	}
}

func TestIncrementalSyncFetchesOnlyNewUIDs(t *testing.T) {
	server := imaptest.New(t, moveCaps())
	now := time.Now()
	server.AddMessage("INBOX", "alt@example.com", "Alt", "alt", now.Add(-2*time.Hour))
	server.AddMessage("INBOX", "alt2@example.com", "Alt2", "alt2", now.Add(-time.Hour))
	client := newTestClient(server)
	account := testAccount(server)

	first, _ := collectSync(t, client, account, nil, SyncOptions{})
	state := syncState(first)

	// No new messages: nothing is fetched.
	second, seen := collectSync(t, client, account, state, SyncOptions{})
	if second.Resync || len(seen) != 0 || second.Processed != 0 {
		t.Fatalf("expected empty incremental sync, resync=%v seen=%d", second.Resync, len(seen))
	}
	if second.LastUID != first.LastUID {
		t.Fatalf("last UID changed: %d -> %d", first.LastUID, second.LastUID)
	}

	// Two new messages: only they are fetched, old UIDs are skipped.
	server.AddMessage("INBOX", "neu@example.com", "Neu", "neu", now)
	server.AddMessage("INBOX", "neu2@example.com", "Neu2", "neu2", now)
	third, seen := collectSync(t, client, account, state, SyncOptions{})
	if third.Resync {
		t.Fatal("incremental sync must not be a resync")
	}
	if len(seen) != 2 {
		t.Fatalf("expected 2 new messages, saw %d", len(seen))
	}
	for _, features := range seen {
		if features.UID <= first.LastUID {
			t.Fatalf("old UID %d refetched", features.UID)
		}
	}
	if third.LastUID != first.LastUID+2 {
		t.Fatalf("last UID = %d, want %d", third.LastUID, first.LastUID+2)
	}
}

func TestUIDValidityChangeTriggersSafeResync(t *testing.T) {
	server := imaptest.New(t, moveCaps())
	now := time.Now()
	server.AddMessage("INBOX", "alt@example.com", "Alt", "alt", now.Add(-time.Hour))
	client := newTestClient(server)
	account := testAccount(server)

	first, _ := collectSync(t, client, account, nil, SyncOptions{})
	state := syncState(first)

	// The server recreates the mailbox: every stored UID becomes invalid.
	server.RecreateMailbox("INBOX")
	server.AddMessage("INBOX", "frisch@example.com", "Frisch 1", "eins", now)
	server.AddMessage("INBOX", "frisch2@example.com", "Frisch 2", "zwei", now)

	second, seen := collectSync(t, client, account, state, SyncOptions{})
	if !second.Resync {
		t.Fatal("UIDVALIDITY change must trigger a resync")
	}
	if second.UIDValidity == first.UIDValidity {
		t.Fatal("UIDVALIDITY did not change on the server")
	}
	if len(seen) != 2 {
		t.Fatalf("expected full re-read of 2 messages, saw %d", len(seen))
	}
	if second.LastUID != 2 {
		t.Fatalf("last UID = %d, want 2", second.LastUID)
	}
}

func TestSyncFetchesBoundedText(t *testing.T) {
	server := imaptest.New(t, moveCaps())
	server.AddMessage("INBOX", "autor@example.com", "Betreff", "Geheimer Antrag: Sofort handeln und Konto bestätigen.", time.Now())
	client := newTestClient(server)

	var text string
	outcome, err := client.SyncFolder(context.Background(), testAccount(server), imaptest.Password, "INBOX", nil, SyncOptions{FetchText: true}, func(_ domain.MessageFeatures, body string) error {
		text = body
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Processed != 1 {
		t.Fatalf("processed = %d, want 1", outcome.Processed)
	}
	if !strings.Contains(text, "Sofort handeln") {
		t.Fatalf("text not fetched: %q", text)
	}
}

func TestSyncCancellationStopsRun(t *testing.T) {
	server := imaptest.New(t, moveCaps())
	for i := 0; i < 3; i++ {
		server.AddMessage("INBOX", "sender@example.com", "Nachricht", "Inhalt", time.Now())
	}
	client := newTestClient(server)
	ctx, cancel := context.WithCancel(context.Background())
	count := 0
	_, err := client.SyncFolder(ctx, testAccount(server), imaptest.Password, "INBOX", nil, SyncOptions{}, func(domain.MessageFeatures, string) error {
		count++
		cancel()
		return nil
	})
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	if count != 1 {
		t.Fatalf("handler ran %d times, want 1", count)
	}
}

func TestSyncConnectionFailure(t *testing.T) {
	server := imaptest.New(t, moveCaps())
	client := newTestClient(server)
	account := testAccount(server)
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := client.SyncFolder(ctx, account, imaptest.Password, "INBOX", nil, SyncOptions{}, func(domain.MessageFeatures, string) error { return nil })
	if err == nil {
		t.Fatal("expected connection error after server shutdown")
	}
}

func TestMoveAtomicMovesMessageAndReportsDestUID(t *testing.T) {
	server := imaptest.New(t, moveCaps())
	uid := server.AddMessage("INBOX", "spam@example.com", "Spam", "spam", time.Now())
	client := newTestClient(server)
	account := testAccount(server)

	destination, err := client.MoveAtomic(context.Background(), account, imaptest.Password, "INBOX", uid, "AI_SPAM_FILTER")
	if err != nil {
		t.Fatalf("move failed: %v", err)
	}
	if destination == 0 {
		t.Fatal("server did not report a destination UID")
	}
	if len(server.Snapshot("INBOX")) != 0 {
		t.Fatal("message still present in INBOX after MOVE")
	}
	moved := server.Snapshot("AI_SPAM_FILTER")
	if len(moved) != 1 || moved[0].UID != destination {
		t.Fatalf("destination mismatch: %+v vs %d", moved, destination)
	}
}

func TestMoveRefusedWithoutMoveCapability(t *testing.T) {
	server := imaptest.New(t, noMoveCaps())
	uid := server.AddMessage("INBOX", "spam@example.com", "Spam", "spam", time.Now())
	client := newTestClient(server)

	_, err := client.MoveAtomic(context.Background(), testAccount(server), imaptest.Password, "INBOX", uid, "AI_SPAM_FILTER")
	if !errors.Is(err, ErrMoveUnsupported) {
		t.Fatalf("expected ErrMoveUnsupported, got %v", err)
	}
	if len(server.Snapshot("INBOX")) != 1 {
		t.Fatal("message must remain untouched when MOVE is unsupported")
	}
	if len(server.Snapshot("AI_SPAM_FILTER")) != 0 {
		t.Fatal("message appeared in spam folder despite unsupported MOVE")
	}
}

func TestWrongPasswordIsRejected(t *testing.T) {
	server := imaptest.New(t, moveCaps())
	client := newTestClient(server)
	account := testAccount(server)
	_, err := client.SyncFolder(context.Background(), account, "falsch", "INBOX", nil, SyncOptions{}, func(domain.MessageFeatures, string) error { return nil })
	if err == nil {
		t.Fatal("expected authentication error")
	}
}

func TestTestConnectionReportsCapabilities(t *testing.T) {
	server := imaptest.New(t, moveCaps())
	client := newTestClient(server)
	info, err := client.TestConnection(context.Background(), testAccount(server), imaptest.Password)
	if err != nil {
		t.Fatal(err)
	}
	if !info.SupportsMove || !info.SupportsIdle {
		t.Fatalf("capabilities not reported: %+v", info)
	}
	if len(info.Folders) != 3 {
		t.Fatalf("folders = %v", info.Folders)
	}
}
