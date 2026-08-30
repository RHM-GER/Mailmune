package mailbox

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/RHM-GER/Mailmune/internal/classifier"
	"github.com/RHM-GER/Mailmune/internal/domain"
)

var ErrMoveUnsupported = errors.New("server does not support atomic IMAP MOVE")

type Client struct{ timeout time.Duration }

type ConnectionInfo struct {
	Folders      []string `json:"folders"`
	SupportsIdle bool     `json:"supportsIdle"`
	SupportsMove bool     `json:"supportsMove"`
}

func NewClient() *Client { return &Client{timeout: 30 * time.Second} }

func (m *Client) connect(account domain.AccountConfig, password string) (*imapclient.Client, error) {
	if account.Port <= 0 {
		account.Port = 993
	}
	host := strings.TrimSpace(account.Host)
	if host == "" || password == "" {
		return nil, errors.New("host and password are required")
	}
	address := net.JoinHostPort(host, strconv.Itoa(account.Port))
	options := &imapclient.Options{TLSConfig: &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}, Dialer: &net.Dialer{Timeout: m.timeout, KeepAlive: 30 * time.Second}}
	client, err := imapclient.DialTLS(address, options)
	if err != nil {
		return nil, fmt.Errorf("secure IMAP connection: %w", err)
	}
	if err := client.Login(account.Username, password).Wait(); err != nil {
		client.Close()
		return nil, fmt.Errorf("IMAP login: %w", err)
	}
	return client, nil
}

func (m *Client) TestConnection(ctx context.Context, account domain.AccountConfig, password string) (ConnectionInfo, error) {
	type result struct {
		info ConnectionInfo
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		client, err := m.connect(account, password)
		if err != nil {
			ch <- result{err: err}
			return
		}
		defer client.Close()
		defer client.Logout().Wait()
		boxes, err := client.List("", "*", nil).Collect()
		if err != nil {
			ch <- result{err: fmt.Errorf("list folders: %w", err)}
			return
		}
		info := ConnectionInfo{SupportsIdle: client.Caps().Has(imap.CapIdle), SupportsMove: client.Caps().Has(imap.CapMove)}
		for _, box := range boxes {
			info.Folders = append(info.Folders, box.Mailbox)
		}
		ch <- result{info: info}
	}()
	select {
	case <-ctx.Done():
		return ConnectionInfo{}, ctx.Err()
	case value := <-ch:
		return value.info, value.err
	}
}

// ScanMetadata reads envelopes, selected headers and attachment metadata only.
// It never changes flags, folders or message contents.
func (m *Client) ScanMetadata(ctx context.Context, account domain.AccountConfig, password, folder string, maxMessages int, since time.Time) ([]domain.MessageFeatures, uint32, error) {
	if maxMessages <= 0 || maxMessages > 1000 {
		maxMessages = 1000
	}
	type result struct {
		messages []domain.MessageFeatures
		validity uint32
		err      error
	}
	ch := make(chan result, 1)
	go func() {
		client, err := m.connect(account, password)
		if err != nil {
			ch <- result{err: err}
			return
		}
		defer client.Close()
		defer client.Logout().Wait()
		selected, err := client.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait()
		if err != nil {
			ch <- result{err: fmt.Errorf("select %s read-only: %w", folder, err)}
			return
		}
		if selected.NumMessages == 0 {
			ch <- result{validity: selected.UIDValidity}
			return
		}
		start := uint32(1)
		if selected.NumMessages > uint32(maxMessages) {
			start = selected.NumMessages - uint32(maxMessages) + 1
		}
		set := imap.SeqSet{}
		set.AddRange(start, selected.NumMessages)
		headerSection := &imap.FetchItemBodySection{Specifier: imap.PartSpecifierHeader, HeaderFields: []string{"List-Unsubscribe", "List-Id", "Auto-Submitted", "Reply-To", "Message-ID"}, Peek: true, Partial: &imap.SectionPartial{Offset: 0, Size: 64 << 10}}
		options := &imap.FetchOptions{UID: true, Envelope: true, InternalDate: true, RFC822Size: true, BodyStructure: &imap.FetchItemBodyStructure{Extended: true}, BodySection: []*imap.FetchItemBodySection{headerSection}}
		fetched, err := client.Fetch(set, options).Collect()
		if err != nil {
			ch <- result{err: fmt.Errorf("fetch metadata: %w", err)}
			return
		}
		messages := make([]domain.MessageFeatures, 0, len(fetched))
		for _, item := range fetched {
			if item.Envelope == nil || (!since.IsZero() && item.InternalDate.Before(since)) {
				continue
			}
			feature := domain.MessageFeatures{AccountID: account.ID, UIDValidity: selected.UIDValidity, UID: uint32(item.UID), Folder: folder, Subject: item.Envelope.Subject, MessageID: item.Envelope.MessageID, ReceivedAt: item.InternalDate}
			if len(item.Envelope.From) > 0 {
				feature.From = item.Envelope.From[0].Addr()
				feature.FromDomain = classifier.ExtractDomain(feature.From)
			}
			if len(item.Envelope.ReplyTo) > 0 {
				feature.ReplyTo = item.Envelope.ReplyTo[0].Addr()
			}
			headerBytes := item.FindBodySection(headerSection)
			if len(headerBytes) > 0 {
				if parsed, err := mail.ReadMessage(bytes.NewReader(headerBytes)); err == nil {
					feature.ListUnsubscribe = parsed.Header.Get("List-Unsubscribe") != ""
				}
			}
			if item.BodyStructure != nil {
				item.BodyStructure.Walk(func(_ []int, part imap.BodyStructure) bool {
					single, ok := part.(*imap.BodyStructureSinglePart)
					if !ok {
						return true
					}
					filename := single.Filename()
					if filename != "" {
						feature.Attachments = append(feature.Attachments, domain.AttachmentMetadata{Filename: filename, MIMEType: single.MediaType(), Size: int64(single.Size)})
					}
					return true
				})
			}
			messages = append(messages, feature)
		}
		ch <- result{messages: messages, validity: selected.UIDValidity}
	}()
	select {
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	case value := <-ch:
		return value.messages, value.validity, value.err
	}
}

// MoveAtomic deliberately refuses the library's COPY+STORE+EXPUNGE fallback.
// This prevents expunging unrelated messages and preserves the no-delete invariant.
func (m *Client) MoveAtomic(ctx context.Context, account domain.AccountConfig, password, origin string, uid uint32, target string) (uint32, error) {
	type result struct {
		uid uint32
		err error
	}
	ch := make(chan result, 1)
	go func() {
		client, err := m.connect(account, password)
		if err != nil {
			ch <- result{err: err}
			return
		}
		defer client.Close()
		defer client.Logout().Wait()
		if !client.Caps().Has(imap.CapMove) {
			ch <- result{err: ErrMoveUnsupported}
			return
		}
		if _, err := client.Select(origin, nil).Wait(); err != nil {
			ch <- result{err: err}
			return
		}
		data, err := client.Move(imap.UIDSetNum(imap.UID(uid)), target).Wait()
		if err != nil {
			ch <- result{err: fmt.Errorf("atomic move: %w", err)}
			return
		}
		var destination uint32
		if data != nil {
			if set, ok := data.DestUIDs.(imap.UIDSet); ok {
				if nums, complete := set.Nums(); complete && len(nums) == 1 {
					destination = uint32(nums[0])
				}
			}
		}
		ch <- result{uid: destination}
	}()
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case value := <-ch:
		return value.uid, value.err
	}
}
