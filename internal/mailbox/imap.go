package mailbox

import (
	"context"
	"crypto/tls"
	"crypto/x509"
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
// disable verification. roots only replaces the set of trusted certificate
// authorities (used by tests with a self-signed CA) and never weakens
// verification.
type Client struct {
	timeout time.Duration
	roots   *x509.CertPool
}

type ConnectionInfo struct {
	Folders      []string `json:"folders"`
	SupportsIdle bool     `json:"supportsIdle"`
	SupportsMove bool     `json:"supportsMove"`
}

func NewClient() *Client { return &Client{timeout: 30 * time.Second} }

// NewClientWithRoots returns a client that trusts the given root CAs instead
// of the system pool. Verification, hostname checking and the TLS 1.2 minimum
// remain fully enforced. Only tests use this with a self-signed CA.
func NewClientWithRoots(roots *x509.CertPool, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{timeout: timeout, roots: roots}
}

func (m *Client) tlsConfig(host string) *tls.Config {
	cfg := &tls.Config{RootCAs: m.roots}
	cfg.InsecureSkipVerify = false
	if cfg.MinVersion < tls.VersionTLS12 {
		cfg.MinVersion = tls.VersionTLS12
	}
	cfg.ServerName = host
	return cfg
}

func (m *Client) connect(ctx context.Context, account domain.AccountConfig, password string) (*imapclient.Client, error) {
	if account.Port <= 0 {
		account.Port = 993
	}
	host := strings.TrimSpace(account.Host)
	if host == "" || password == "" {
		return nil, errors.New("host and password are required")
	}
	address := net.JoinHostPort(host, strconv.Itoa(account.Port))
	// Bound dial and TLS handshake independently of the caller context so a
	// half-open connection can never stall a run indefinitely.
	dialCtx, cancelDial := context.WithTimeout(ctx, m.timeout)
	defer cancelDial()
	dialer := &net.Dialer{Timeout: m.timeout, KeepAlive: 30 * time.Second}
	rawConn, err := dialer.DialContext(dialCtx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("secure IMAP connection: %w", err)
	}
	tlsConn := tls.Client(rawConn, m.tlsConfig(host))
	if err := tlsConn.HandshakeContext(dialCtx); err != nil {
		rawConn.Close()
		return nil, fmt.Errorf("secure IMAP connection: %w", err)
	}
	client := imapclient.New(tlsConn, nil)
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

// TestConnection performs a read-only connection test: login, list folders
// and report MOVE/IDLE availability. No messages are touched.
func (m *Client) TestConnection(ctx context.Context, account domain.AccountConfig, password string) (ConnectionInfo, error) {
	if err := ctx.Err(); err != nil {
		return ConnectionInfo{}, err
	}
	client, err := m.connect(ctx, account, password)
	if err != nil {
		return ConnectionInfo{}, err
	}
	defer client.Close()
	defer func() { _ = client.Logout().Wait() }()
	boxes, err := client.List("", "*", nil).Collect()
	if err != nil {
		return ConnectionInfo{}, fmt.Errorf("list folders: %w", err)
	}
	info := ConnectionInfo{SupportsIdle: supportsIdle(client.Caps()), SupportsMove: supportsMove(client.Caps())}
	for _, box := range boxes {
		info.Folders = append(info.Folders, box.Mailbox)
	}
	return info, nil
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
// This prevents expunging unrelated messages and preserves the no-delete
// invariant. The call runs synchronously so a cancelled run can never leave
// an orphaned move in flight.
func (m *Client) MoveAtomic(ctx context.Context, account domain.AccountConfig, password, origin string, uid uint32, target string) (uint32, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	client, err := m.connect(ctx, account, password)
	if err != nil {
		return 0, err
	}
	defer client.Close()
	defer func() { _ = client.Logout().Wait() }()
	if !supportsMove(client.Caps()) {
		return 0, ErrMoveUnsupported
	}
	if _, err := client.Select(origin, nil).Wait(); err != nil {
		return 0, err
	}
	data, err := client.Move(imap.UIDSetNum(imap.UID(uid)), target).Wait()
	if err != nil {
		return 0, fmt.Errorf("atomic move: %w", err)
	}
	var destination uint32
	if data != nil {
		if set, ok := data.DestUIDs.(imap.UIDSet); ok {
			if nums, complete := set.Nums(); complete && len(nums) == 1 {
				destination = uint32(nums[0])
			}
		}
	}
	return destination, nil
}
