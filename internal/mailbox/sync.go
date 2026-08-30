package mailbox

import (
	"context"
	"fmt"
	"io"
	"net/mail"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/RHM-GER/Mailmune/internal/classifier"
	"github.com/RHM-GER/Mailmune/internal/domain"
)

const (
	// Hard limits protecting the agent from resource exhaustion.
	DefaultMaxMessages = 1000
	headerPartialSize  = int64(64 << 10)
	defaultTextLimit   = int64(16 << 10)
	maxMimeDepth       = 8
	maxMimeParts       = 256

	// Selected headers that are safe metadata and useful for classification.
	scanHeaderFields = "List-Unsubscribe,List-Id,Auto-Submitted,Reply-To,Message-ID,Authentication-Results,Precedence"
)

// SyncOptions bounds a single folder synchronization run.
type SyncOptions struct {
	// MaxMessages caps the initial (re-)sync. It is ignored for incremental
	// syncs, which only fetch UIDs above the persisted state.
	MaxMessages int
	// Since bounds the initial sync by internal date. It is ignored for
	// incremental syncs.
	Since time.Time
	// FetchText additionally loads a bounded plain-text representation for
	// statistical and model classification.
	FetchText bool
	// TextLimit bounds the fetched text part in bytes.
	TextLimit int64
	// OnProgress reports (processed, estimatedTotal) after each message.
	OnProgress func(processed, estimatedTotal int)
}

// SyncOutcome is the result of one synchronization run.
type SyncOutcome struct {
	UIDValidity uint32
	LastUID     uint32
	// Resync is true when UIDVALIDITY changed or the folder had no state.
	// The caller must treat this as a safe re-synchronization, never as a
	// continuation.
	Resync    bool
	Processed int
	Total     int
}

// MessageHandler receives every observed message. The text argument is empty
// unless SyncOptions.FetchText is set; it must not be retained. Returning an
// error aborts the run.
type MessageHandler func(msg domain.MessageFeatures, text string) error

// fetchedMessage holds the metadata of a single message consumed from the
// streaming FETCH response. It is discarded after classification.
type fetchedMessage struct {
	uid           imap.UID
	envelope      *imap.Envelope
	internalDate  time.Time
	bodyStructure imap.BodyStructure
	headerBytes   []byte
}

// SyncFolder reads a folder UID-based and read-only. It fetches only UIDs
// above the persisted state; a UIDVALIDITY change triggers a bounded
// re-synchronization from the most recent messages instead of a blind
// continuation. Flags, folders and message contents are never modified.
func (m *Client) SyncFolder(ctx context.Context, account domain.AccountConfig, password, folder string, prev *domain.FolderSyncState, opts SyncOptions, handler MessageHandler) (*SyncOutcome, error) {
	type result struct {
		outcome *SyncOutcome
		err     error
	}
	ch := make(chan result, 1)
	go func() {
		outcome, err := m.syncFolder(ctx, account, password, folder, prev, opts, handler)
		ch <- result{outcome: outcome, err: err}
	}()
	select {
	case <-ctx.Done():
		if prev != nil {
			return &SyncOutcome{UIDValidity: prev.UIDValidity, LastUID: prev.LastUID}, ctx.Err()
		}
		return &SyncOutcome{}, ctx.Err()
	case value := <-ch:
		return value.outcome, value.err
	}
}

func (m *Client) syncFolder(ctx context.Context, account domain.AccountConfig, password, folder string, prev *domain.FolderSyncState, opts SyncOptions, handler MessageHandler) (*SyncOutcome, error) {
	client, err := m.connect(account, password)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	defer func() { _ = client.Logout().Wait() }()

	selected, err := client.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return nil, fmt.Errorf("select %s read-only: %w", folder, err)
	}

	resync := prev == nil || prev.UIDValidity == 0 || prev.UIDValidity != selected.UIDValidity
	outcome := &SyncOutcome{UIDValidity: selected.UIDValidity, Resync: resync}
	if prev != nil && !resync {
		outcome.LastUID = prev.LastUID
	}

	if selected.NumMessages == 0 {
		return outcome, nil
	}
	// UIDNEXT may be absent on minimal servers; only trust it when present.
	if !resync && selected.UIDNext != 0 && imap.UID(prev.LastUID)+1 >= selected.UIDNext {
		return outcome, nil
	}

	headerSection := &imap.FetchItemBodySection{Specifier: imap.PartSpecifierHeader, HeaderFields: strings.Split(scanHeaderFields, ","), Peek: true, Partial: &imap.SectionPartial{Offset: 0, Size: headerPartialSize}}
	fetchOptions := &imap.FetchOptions{UID: true, Envelope: true, InternalDate: true, RFC822Size: true, BodyStructure: &imap.FetchItemBodyStructure{Extended: true}, BodySection: []*imap.FetchItemBodySection{headerSection}}

	var cmd *imapclient.FetchCommand
	if resync {
		maxMessages := opts.MaxMessages
		if maxMessages <= 0 || maxMessages > DefaultMaxMessages {
			maxMessages = DefaultMaxMessages
		}
		start := uint32(1)
		if selected.NumMessages > uint32(maxMessages) {
			start = selected.NumMessages - uint32(maxMessages) + 1
		}
		var set imap.SeqSet
		set.AddRange(start, 0)
		cmd = client.Fetch(set, fetchOptions)
		outcome.Total = int(selected.NumMessages - start + 1)
	} else {
		var set imap.UIDSet
		set.AddRange(imap.UID(prev.LastUID)+1, 0)
		cmd = client.Fetch(set, fetchOptions)
		outcome.Total = int(selected.UIDNext) - int(prev.LastUID) - 1
		if outcome.Total < 0 {
			outcome.Total = 0
		}
	}
	return m.consumeFetch(ctx, client, cmd, headerSection, resync, opts, account, folder, selected.UIDValidity, outcome, handler)
}

func (m *Client) consumeFetch(ctx context.Context, client *imapclient.Client, cmd *imapclient.FetchCommand, headerSection *imap.FetchItemBodySection, resync bool, opts SyncOptions, account domain.AccountConfig, folder string, uidValidity uint32, outcome *SyncOutcome, handler MessageHandler) (*SyncOutcome, error) {
	for {
		if err := ctx.Err(); err != nil {
			cmd.Close()
			return outcome, err
		}
		message := cmd.Next()
		if message == nil {
			break
		}
		fetched, err := consumeMessage(message, headerSection)
		if err != nil {
			cmd.Close()
			return outcome, err
		}
		if fetched.envelope == nil || fetched.uid == 0 {
			continue
		}
		// Track every observed UID, including date-filtered ones, so the
		// next run does not re-fetch them.
		if uint32(fetched.uid) > outcome.LastUID {
			outcome.LastUID = uint32(fetched.uid)
		}
		if resync && !opts.Since.IsZero() && fetched.internalDate.Before(opts.Since) {
			continue
		}

		features := buildFeatures(account, folder, uidValidity, fetched)
		var text string
		if opts.FetchText {
			text = m.fetchText(client, fetched, opts.TextLimit)
		}
		if err := handler(features, text); err != nil {
			cmd.Close()
			return outcome, err
		}
		outcome.Processed++
		if opts.OnProgress != nil {
			opts.OnProgress(outcome.Processed, outcome.Total)
		}
	}
	if err := cmd.Close(); err != nil {
		return outcome, fmt.Errorf("fetch metadata: %w", err)
	}
	return outcome, nil
}

// consumeMessage drains all data items of one streamed FETCH response.
func consumeMessage(message *imapclient.FetchMessageData, headerSection *imap.FetchItemBodySection) (*fetchedMessage, error) {
	fetched := &fetchedMessage{}
	for {
		item := message.Next()
		if item == nil {
			break
		}
		switch value := item.(type) {
		case imapclient.FetchItemDataUID:
			fetched.uid = value.UID
		case imapclient.FetchItemDataEnvelope:
			fetched.envelope = value.Envelope
		case imapclient.FetchItemDataInternalDate:
			fetched.internalDate = value.Time
		case imapclient.FetchItemDataBodyStructure:
			fetched.bodyStructure = value.BodyStructure
		case imapclient.FetchItemDataBodySection:
			if value.Literal == nil {
				continue
			}
			if value.MatchCommand(headerSection) {
				data, err := readLiteralBounded(value.Literal, headerPartialSize+1024)
				if err != nil {
					return nil, err
				}
				fetched.headerBytes = data
			} else {
				// Drain unexpected sections so the stream stays consistent.
				if _, err := io.Copy(io.Discard, io.LimitReader(value.Literal, defaultTextLimit+1024)); err != nil {
					return nil, err
				}
			}
		default:
			// Flags, sizes and other items are not needed for the scan.
		}
	}
	return fetched, nil
}

func readLiteralBounded(reader io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, limit))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) >= limit {
		return data[:0], nil
	}
	return data, nil
}

func buildFeatures(account domain.AccountConfig, folder string, uidValidity uint32, fetched *fetchedMessage) domain.MessageFeatures {
	features := domain.MessageFeatures{AccountID: account.ID, UIDValidity: uidValidity, UID: uint32(fetched.uid), Folder: folder, Subject: fetched.envelope.Subject, MessageID: fetched.envelope.MessageID, ReceivedAt: fetched.internalDate}
	if len(fetched.envelope.From) > 0 {
		features.From = fetched.envelope.From[0].Addr()
		features.FromDomain = classifier.ExtractDomain(features.From)
	}
	if len(fetched.envelope.ReplyTo) > 0 {
		features.ReplyTo = fetched.envelope.ReplyTo[0].Addr()
	}
	if len(fetched.headerBytes) > 0 {
		if parsed, err := mail.ReadMessage(strings.NewReader(string(fetched.headerBytes) + "\r\n")); err == nil {
			features.ListUnsubscribe = parsed.Header.Get("List-Unsubscribe") != ""
			if auth := parsed.Header.Get("Authentication-Results"); auth != "" {
				// Authentication-Results is untrusted input; treat it only as
				// a weak hint, never as proof.
				lower := strings.ToLower(auth)
				if strings.Contains(lower, "spf=pass") || strings.Contains(lower, "dkim=pass") || strings.Contains(lower, "dmarc=pass") {
					features.AuthenticationPassed = true
				}
				if strings.Contains(lower, "spf=fail") || strings.Contains(lower, "dkim=fail") || strings.Contains(lower, "dmarc=fail") {
					features.AuthenticationFailed = true
				}
			}
		}
	}
	if fetched.bodyStructure != nil {
		parts := 0
		fetched.bodyStructure.Walk(func(_ []int, part imap.BodyStructure) bool {
			parts++
			if parts > maxMimeParts {
				return false
			}
			single, ok := part.(*imap.BodyStructureSinglePart)
			if !ok {
				return true
			}
			if filename := single.Filename(); filename != "" {
				features.Attachments = append(features.Attachments, domain.AttachmentMetadata{Filename: filename, MIMEType: single.MediaType(), Size: int64(single.Size)})
			}
			return true
		})
	}
	return features
}

// fetchText loads a bounded plain-text representation of the message body.
// It prefers the first text/plain part, falls back to text/html converted
// offline, and finally to BODY[TEXT]. Nothing is resolved or fetched from
// external sources, and the caller must not retain the result.
func (m *Client) fetchText(client *imapclient.Client, fetched *fetchedMessage, limit int64) string {
	if limit <= 0 || limit > defaultTextLimit {
		limit = defaultTextLimit
	}
	var path []int
	media := ""
	if fetched.bodyStructure != nil {
		parts := 0
		fetched.bodyStructure.Walk(func(partPath []int, part imap.BodyStructure) bool {
			parts++
			if parts > maxMimeParts || len(partPath) > maxMimeDepth {
				return false
			}
			single, ok := part.(*imap.BodyStructureSinglePart)
			if !ok {
				return true
			}
			switch single.MediaType() {
			case "text/plain":
				path = append([]int(nil), partPath...)
				media = "text/plain"
				return false
			case "text/html":
				if path == nil {
					path = append([]int(nil), partPath...)
					media = "text/html"
				}
			}
			return true
		})
	}

	var section *imap.FetchItemBodySection
	if path != nil {
		section = &imap.FetchItemBodySection{Part: path, Peek: true, Partial: &imap.SectionPartial{Offset: 0, Size: limit}}
	} else {
		section = &imap.FetchItemBodySection{Specifier: imap.PartSpecifierText, Peek: true, Partial: &imap.SectionPartial{Offset: 0, Size: limit}}
	}
	cmd := client.Fetch(imap.UIDSetNum(fetched.uid), &imap.FetchOptions{BodySection: []*imap.FetchItemBodySection{section}})
	text := drainTextFetch(cmd, section, limit)
	if media == "text/html" {
		return HTMLToText(text)
	}
	return text
}

func drainTextFetch(cmd *imapclient.FetchCommand, section *imap.FetchItemBodySection, limit int64) string {
	defer cmd.Close()
	message := cmd.Next()
	if message == nil {
		return ""
	}
	var text []byte
	for {
		item := message.Next()
		if item == nil {
			break
		}
		value, ok := item.(imapclient.FetchItemDataBodySection)
		if !ok || value.Literal == nil {
			continue
		}
		if !value.MatchCommand(section) {
			_, _ = io.Copy(io.Discard, io.LimitReader(value.Literal, limit+1024))
			continue
		}
		data, err := readLiteralBounded(value.Literal, limit+1024)
		if err == nil {
			text = data
		}
	}
	return string(text)
}
