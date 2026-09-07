package mailbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/RHM-GER/Mailmune/internal/mailbox/imaptest"
)

func TestMoveVerifiedHappyPath(t *testing.T) {
	server := imaptest.New(t, moveCaps())
	uid := server.AddMessage("INBOX", "spam@example.com", "Spam", "body", time.Now())
	client := newTestClient(server)
	account := testAccount(server)
	state := server.MailboxState("INBOX")

	result, err := client.MoveVerified(context.Background(), account, imaptest.Password, "INBOX", uid, state.UIDValidity, "AI_SPAM_FILTER", false)
	if err != nil {
		t.Fatalf("move failed: %v", err)
	}
	if !result.Confirmed || result.DestUID == 0 {
		t.Fatalf("move not confirmed: %+v", result)
	}
	if len(server.Snapshot("INBOX")) != 0 {
		t.Fatal("message still in INBOX after verified move")
	}
	moved := server.Snapshot("AI_SPAM_FILTER")
	if len(moved) != 1 || moved[0].UID != result.DestUID {
		t.Fatalf("destination mismatch: %+v vs %+v", moved, result)
	}
}

func TestMoveVerifiedDetectsUIDValidityChange(t *testing.T) {
	server := imaptest.New(t, moveCaps())
	uid := server.AddMessage("INBOX", "spam@example.com", "Spam", "body", time.Now())
	client := newTestClient(server)
	account := testAccount(server)

	// A stale validity from before a server-side rebuild.
	staleValidity := uint32(1)
	_, err := client.MoveVerified(context.Background(), account, imaptest.Password, "INBOX", uid, staleValidity, "AI_SPAM_FILTER", false)
	if !errors.Is(err, ErrUIDValidityChanged) {
		t.Fatalf("expected ErrUIDValidityChanged, got %v", err)
	}
	if len(server.Snapshot("INBOX")) != 1 {
		t.Fatal("message must not move when UIDVALIDITY changed")
	}
}

func TestMoveVerifiedDetectsGoneMessage(t *testing.T) {
	server := imaptest.New(t, moveCaps())
	server.AddMessage("INBOX", "spam@example.com", "Spam", "body", time.Now())
	client := newTestClient(server)
	account := testAccount(server)
	state := server.MailboxState("INBOX")

	// UID 999 does not exist.
	_, err := client.MoveVerified(context.Background(), account, imaptest.Password, "INBOX", 999, state.UIDValidity, "AI_SPAM_FILTER", false)
	if !errors.Is(err, ErrMessageGone) {
		t.Fatalf("expected ErrMessageGone, got %v", err)
	}
}

func TestMoveVerifiedRefusesWithoutMoveCapability(t *testing.T) {
	server := imaptest.New(t, noMoveCaps())
	uid := server.AddMessage("INBOX", "spam@example.com", "Spam", "body", time.Now())
	client := newTestClient(server)
	account := testAccount(server)
	state := server.MailboxState("INBOX")

	_, err := client.MoveVerified(context.Background(), account, imaptest.Password, "INBOX", uid, state.UIDValidity, "AI_SPAM_FILTER", false)
	if !errors.Is(err, ErrMoveUnsupported) {
		t.Fatalf("expected ErrMoveUnsupported, got %v", err)
	}
	if len(server.Snapshot("INBOX")) != 1 {
		t.Fatal("message must remain when MOVE is unsupported")
	}
}

func TestMoveVerifiedCreatesFolderOnlyWhenAuthorized(t *testing.T) {
	server := imaptest.New(t, moveCaps())
	uid := server.AddMessage("INBOX", "spam@example.com", "Spam", "body", time.Now())
	client := newTestClient(server)
	account := testAccount(server)
	state := server.MailboxState("INBOX")

	// Missing folder without authorization -> refused, nothing moved.
	_, err := client.MoveVerified(context.Background(), account, imaptest.Password, "INBOX", uid, state.UIDValidity, "NeuerOrdner", false)
	if !errors.Is(err, ErrFolderMissing) {
		t.Fatalf("expected ErrFolderMissing, got %v", err)
	}
	if len(server.Snapshot("INBOX")) != 1 {
		t.Fatal("message moved despite missing folder")
	}

	// With authorization the folder is created and the move succeeds.
	result, err := client.MoveVerified(context.Background(), account, imaptest.Password, "INBOX", uid, state.UIDValidity, "NeuerOrdner", true)
	if err != nil {
		t.Fatalf("authorized create+move failed: %v", err)
	}
	if !result.Confirmed {
		t.Fatalf("move not confirmed: %+v", result)
	}
	if len(server.Snapshot("NeuerOrdner")) != 1 {
		t.Fatal("message not in newly created folder")
	}
}

func TestMessageExistsForReconcile(t *testing.T) {
	server := imaptest.New(t, moveCaps())
	uid := server.AddMessage("INBOX", "spam@example.com", "Spam", "body", time.Now())
	client := newTestClient(server)
	account := testAccount(server)
	ctx := context.Background()

	exists, validity, err := client.MessageExists(ctx, account, imaptest.Password, "INBOX", uid)
	if err != nil || !exists || validity == 0 {
		t.Fatalf("expected message present: exists=%v validity=%d err=%v", exists, validity, err)
	}

	// After a move, the origin UID is gone: reconciliation reads this as moved.
	if _, err := client.MoveVerified(ctx, account, imaptest.Password, "INBOX", uid, validity, "AI_SPAM_FILTER", false); err != nil {
		t.Fatal(err)
	}
	exists, _, err = client.MessageExists(ctx, account, imaptest.Password, "INBOX", uid)
	if err != nil || exists {
		t.Fatalf("expected message gone after move: exists=%v err=%v", exists, err)
	}
}
