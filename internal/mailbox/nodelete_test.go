package mailbox

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/RHM-GER/Mailmune/internal/mailbox/imaptest"
)

// TestMailboxClientExposesNoDeleteOperations pins product rule #1 ("Mailmune
// deletes never an email") at the only layer that talks IMAP. The client's
// write surface must stay move-only: if a future change adds a delete,
// expunge, trash or retention method, this audit fails the build instead of
// silently introducing a way to destroy mail.
func TestMailboxClientExposesNoDeleteOperations(t *testing.T) {
	forbidden := []string{"delete", "expunge", "trash", "remove", "destroy", "erase", "purge", "retention"}
	clientType := reflect.TypeOf(&Client{})
	for i := 0; i < clientType.NumMethod(); i++ {
		name := clientType.Method(i).Name
		lower := strings.ToLower(name)
		for _, verb := range forbidden {
			if strings.Contains(lower, verb) {
				t.Fatalf("mailbox.Client must never expose a delete-like method, found %q", name)
			}
		}
	}
	// Sanity check: the audit really sees the move-only write surface, so a
	// rename or reflection change cannot make it pass vacuously.
	for _, expected := range []string{"MoveAtomic", "MoveVerified"} {
		if _, ok := clientType.MethodByName(expected); !ok {
			t.Fatalf("expected %s on the client; the no-delete audit may be misconfigured", expected)
		}
	}
}

// TestMovePreservesEveryMessage proves the relocation is a move, not a
// deletion: the total message count across the source and destination folder
// is unchanged, exactly one message lands in the spam folder, and unrelated
// messages keep their UIDs. Nothing is ever expunged.
func TestMovePreservesEveryMessage(t *testing.T) {
	server := imaptest.New(t, moveCaps())
	keep1 := server.AddMessage("INBOX", "a@example.com", "Behalten 1", "body", time.Now())
	moveUID := server.AddMessage("INBOX", "spam@example.com", "Spam", "body", time.Now())
	keep2 := server.AddMessage("INBOX", "b@example.com", "Behalten 2", "body", time.Now())
	client := newTestClient(server)
	account := testAccount(server)

	before := len(server.Snapshot("INBOX")) + len(server.Snapshot("AI_SPAM_FILTER"))
	if before != 3 {
		t.Fatalf("setup: expected 3 messages, got %d", before)
	}

	if _, err := client.MoveAtomic(context.Background(), account, imaptest.Password, "INBOX", moveUID, "AI_SPAM_FILTER"); err != nil {
		t.Fatalf("move failed: %v", err)
	}

	inbox := server.Snapshot("INBOX")
	spam := server.Snapshot("AI_SPAM_FILTER")
	after := len(inbox) + len(spam)
	if after != before {
		t.Fatalf("message count changed during move: before=%d after=%d (deletion is forbidden)", before, after)
	}
	if len(spam) != 1 {
		t.Fatalf("expected exactly one moved message, got %d", len(spam))
	}
	remaining := map[uint32]bool{}
	for _, message := range inbox {
		remaining[message.UID] = true
	}
	if !remaining[keep1] || !remaining[keep2] {
		t.Fatalf("unrelated messages were altered or lost: inbox UIDs %v", remaining)
	}
}
