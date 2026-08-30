package mailbox

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/RHM-GER/Mailmune/internal/domain"
)

var ErrMoveUnsupported = errors.New("server does not support atomic IMAP MOVE")

// Client is a strictly read-optimized IMAP client. Every connection uses TLS
// with full certificate verification; there is no plaintext and no way to
// disable verification. tlsTemplate is only set by tests to provide a trusted
// test CA and never changes the verification requirements.
type Client struct {
	timeout     time.Duration
	tlsTemplate *tls.Config
}

type ConnectionInfo struct {
	Folders      []string `json:"folders"`
	SupportsIdle bool     `json:"supportsIdle"`
	SupportsMove bool     `json:"supportsMove"`
}

func NewClient() *Client { return &Client{timeout: 30 * time.Second} }

func (m *Client) tlsConfig(host string) *tls.Config {
	var cfg *tls.Config
	if m.tlsTemplate != nil {
		cfg = m.tlsTemplate.Clone()
	} else {
		cfg = &tls.Config{}
	}
	cfg.InsecureSkipVerify = false
	if cfg.MinVersion < tls.VersionTLS12 {
		cfg.MinVersion = tls.VersionTLS12
	}
	cfg.ServerName = host
	return cfg
}

func (m *Client) connect(account domain.AccountConfig, password string) (*imapclient.Client, error) {
	if account.Port <= 0 {
		account.Port = 993
	}
	host := strings.TrimSpace(account.Host)
	if host == "" || password == "" {
		return nil, errors.New("host and password are required")
	}
	address := net.JoinHostPort(host, strconv.Itoa(account.Port))
	options := &imapclient.Options{TLSConfig: m.tlsConfig(host), Dialer: &net.Dialer{Timeout: m.timeout, KeepAlive: 30 * time.Second}}
	client, err := imapclient.DialTLS(address, options)
	if err != nil {
		return nil, fmt.Errorf("secure IMAP connection: %w", err)
	}
	if err := client.Login(account.Username, password).Wait(); err != nil {
		client.Close()
		return nil, fmt.Errorf("IMAP login: %w", err)
	}
	// RFC 9051: IMAP4rev2 must be explicitly enabled by the client. MOVE and
	// IDLE are part of the rev2 core and are no longer advertised separately.
	if client.Caps().Has(imap.CapIMAP4rev2) {
		if _, err := client.Enable(imap.CapIMAP4rev2).Wait(); err != nil {
			client.Close()
			return nil, fmt.Errorf("enable IMAP4rev2: %w", err)
		}
	}
	return client, nil
}

// supportsMove reports whether atomic MOVE is available. For IMAP4rev2
// servers MOVE is mandatory and not advertised as a separate capability.
func supportsMove(caps imap.CapSet) bool {
	return caps.Has(imap.CapMove) || caps.Has(imap.CapIMAP4rev2)
}

func supportsIdle(caps imap.CapSet) bool {
	return caps.Has(imap.CapIdle) || caps.Has(imap.CapIMAP4rev2)
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
		defer func() { _ = client.Logout().Wait() }()
		boxes, err := client.List("", "*", nil).Collect()
		if err != nil {
			ch <- result{err: fmt.Errorf("list folders: %w", err)}
			return
		}
		info := ConnectionInfo{SupportsIdle: supportsIdle(client.Caps()), SupportsMove: supportsMove(client.Caps())}
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
// It never changes flags, folders or message contents. It is kept as a thin
// wrapper over SyncFolder for callers that want a bounded metadata snapshot.
func (m *Client) ScanMetadata(ctx context.Context, account domain.AccountConfig, password, folder string, maxMessages int, since time.Time) ([]domain.MessageFeatures, uint32, error) {
	var collected []domain.MessageFeatures
	outcome, err := m.SyncFolder(ctx, account, password, folder, nil, SyncOptions{MaxMessages: maxMessages, Since: since}, func(features domain.MessageFeatures, _ string) error {
		collected = append(collected, features)
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return collected, outcome.UIDValidity, nil
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
		defer func() { _ = client.Logout().Wait() }()
		if !supportsMove(client.Caps()) {
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
