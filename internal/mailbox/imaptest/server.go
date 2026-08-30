// Package imaptest provides a controlled in-process IMAP server used by the
// project's integration tests. It is built on the imapserver package of the
// already vendored go-imap dependency and speaks real IMAP over TLS with a
// self-signed test certificate. It is never imported by production code.
package imaptest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
)

// Username and Password accepted by every test server.
const (
	Username = "user"
	Password = "pass"
)

// TB is the subset of testing.TB used by the test server, so this package
// does not need to import testing outside of tests.
type TB interface {
	Helper()
	Fatal(args ...any)
	Fatalf(format string, args ...any)
	Cleanup(func())
	Logf(format string, args ...any)
	Failed() bool
}

// Server is a controlled IMAP server listening on loopback with implicit
// TLS. Roots is the certificate pool clients must trust; verification stays
// fully enabled.
type Server struct {
	tb TB

	mu           sync.Mutex
	nextValidity uint32
	mailboxes    map[string]*Mailbox

	Addr  string
	Roots *x509.CertPool

	listener net.Listener
	server   *imapserver.Server

	debugMu sync.Mutex
	debug   strings.Builder
}

// Mailbox is an in-memory IMAP mailbox.
type Mailbox struct {
	uidValidity uint32
	nextUID     uint32
	messages    []*Message
}

// Message is a single stored test message.
type Message struct {
	UID          uint32
	FromMailbox  string
	FromHost     string
	Subject      string
	MessageID    string
	InternalDate time.Time
	HeaderExtra  []string
	Body         string
}

// New starts a test IMAP server advertising the given capabilities.
func New(tb TB, caps imap.CapSet) *Server {
	tb.Helper()
	server := &Server{tb: tb, nextValidity: 100, mailboxes: map[string]*Mailbox{}}
	server.mailboxes["INBOX"] = server.newMailboxLocked()
	server.mailboxes["Sent"] = server.newMailboxLocked()
	server.mailboxes["AI_SPAM_FILTER"] = server.newMailboxLocked()

	serverConfig, roots := selfSignedTLS(tb)
	server.Roots = roots
	listener, err := tls.Listen("tcp", "127.0.0.1:0", serverConfig)
	if err != nil {
		tb.Fatal(err)
	}
	server.listener = listener
	server.Addr = listener.Addr().String()
	server.server = imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return &session{server: server}, &imapserver.GreetingData{}, nil
		},
		Caps:        caps,
		DebugWriter: &debugWriter{server: server},
	})
	go func() { _ = server.server.Serve(listener) }()
	tb.Cleanup(func() {
		_ = server.server.Close()
		if tb.Failed() {
			tb.Logf("IMAP session log:\n%s", server.sessionLog())
		}
	})
	return server
}

type debugWriter struct{ server *Server }

// Write must not use the server mutex: debug traffic is written while the
// session handlers hold it.
func (w *debugWriter) Write(p []byte) (int, error) {
	w.server.debugMu.Lock()
	defer w.server.debugMu.Unlock()
	w.server.debug.Write(p)
	return len(p), nil
}

func (s *Server) sessionLog() string {
	s.debugMu.Lock()
	defer s.debugMu.Unlock()
	return s.debug.String()
}

func selfSignedTLS(tb TB) (*tls.Config, *x509.CertPool) {
	tb.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		tb.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Mailmune Test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		DNSNames:              []string{"localhost"},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		tb.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		tb.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(parsed)
	serverConfig := &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12}
	return serverConfig, pool
}

func (s *Server) newMailboxLocked() *Mailbox {
	s.nextValidity++
	return &Mailbox{uidValidity: s.nextValidity, nextUID: 1}
}

// AddMessage appends a message and returns its UID.
func (s *Server) AddMessage(folder, from, subject, body string, when time.Time, headerExtra ...string) uint32 {
	mailboxName, host := splitAddress(from)
	s.mu.Lock()
	defer s.mu.Unlock()
	box := s.mailboxes[folder]
	if box == nil {
		box = s.newMailboxLocked()
		s.mailboxes[folder] = box
	}
	message := &Message{UID: box.nextUID, FromMailbox: mailboxName, FromHost: host, Subject: subject, MessageID: subject + "-" + when.Format("150405.000000") + "@test.invalid", InternalDate: when, HeaderExtra: headerExtra, Body: body}
	box.nextUID++
	box.messages = append(box.messages, message)
	return message.UID
}

// RecreateMailbox simulates a UIDVALIDITY change: every stored UID becomes
// invalid, exactly like a server-side mailbox rebuild.
func (s *Server) RecreateMailbox(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mailboxes[name] = s.newMailboxLocked()
}

// Snapshot returns a copy of the messages currently stored in a folder.
func (s *Server) Snapshot(folder string) []*Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	box := s.mailboxes[folder]
	if box == nil {
		return nil
	}
	return append([]*Message(nil), box.messages...)
}

// Close stops the server immediately. It closes the listener directly in
// addition to the imapserver handle, because a very early Close can race
// the Serve goroutine registering the listener. Tests use this to simulate
// outages.
func (s *Server) Close() error {
	_ = s.server.Close()
	return s.listener.Close()
}

func splitAddress(address string) (string, string) {
	parts := strings.SplitN(address, "@", 2)
	if len(parts) != 2 {
		return address, "invalid"
	}
	return parts[0], parts[1]
}

type session struct {
	server   *Server
	selected string
}

var (
	_ imapserver.Session          = (*session)(nil)
	_ imapserver.SessionMove      = (*session)(nil)
	_ imapserver.SessionIMAP4rev2 = (*session)(nil)
)

func (s *session) Close() error { return nil }

func (s *session) Login(username, password string) error {
	if username != Username || password != Password {
		return imapserver.ErrAuthFailed
	}
	return nil
}

func (s *session) mailboxLocked(name string) *Mailbox {
	return s.server.mailboxes[name]
}

func (s *session) Select(mailbox string, _ *imap.SelectOptions) (*imap.SelectData, error) {
	s.server.mu.Lock()
	defer s.server.mu.Unlock()
	box := s.mailboxLocked(mailbox)
	if box == nil {
		return nil, errors.New("no such mailbox")
	}
	s.selected = mailbox
	return &imap.SelectData{
		Flags:          []imap.Flag{imap.FlagSeen, imap.FlagAnswered, imap.FlagFlagged},
		PermanentFlags: []imap.Flag{imap.FlagSeen, imap.FlagAnswered, imap.FlagFlagged},
		NumMessages:    uint32(len(box.messages)),
		UIDNext:        imap.UID(box.nextUID),
		UIDValidity:    box.uidValidity,
	}, nil
}

func (s *session) Create(mailbox string, _ *imap.CreateOptions) error {
	s.server.mu.Lock()
	defer s.server.mu.Unlock()
	if s.mailboxLocked(mailbox) != nil {
		return errors.New("mailbox exists")
	}
	s.server.mailboxes[mailbox] = s.server.newMailboxLocked()
	return nil
}

func (s *session) Delete(string) error                            { return errors.New("deletion is not supported") }
func (s *session) Rename(string, string, *imap.RenameOptions) error { return errors.New("rename is not supported") }
func (s *session) Subscribe(string) error                         { return nil }
func (s *session) Unsubscribe(string) error                       { return nil }

func (s *session) Namespace() (*imap.NamespaceData, error) {
	return &imap.NamespaceData{Personal: []imap.NamespaceDescriptor{{Prefix: "", Delim: '/'}}}, nil
}

func (s *session) List(w *imapserver.ListWriter, _ string, _ []string, _ *imap.ListOptions) error {
	s.server.mu.Lock()
	defer s.server.mu.Unlock()
	for name := range s.server.mailboxes {
		if err := w.WriteList(&imap.ListData{Attrs: []imap.MailboxAttr{}, Delim: '/', Mailbox: name}); err != nil {
			return err
		}
	}
	return nil
}

func (s *session) Status(mailbox string, _ *imap.StatusOptions) (*imap.StatusData, error) {
	s.server.mu.Lock()
	defer s.server.mu.Unlock()
	box := s.mailboxLocked(mailbox)
	if box == nil {
		return nil, errors.New("no such mailbox")
	}
	numMessages := uint32(len(box.messages))
	return &imap.StatusData{Mailbox: mailbox, UIDValidity: box.uidValidity, UIDNext: imap.UID(box.nextUID), NumMessages: &numMessages}, nil
}

func (s *session) Append(string, imap.LiteralReader, *imap.AppendOptions) (*imap.AppendData, error) {
	return nil, errors.New("append is not supported")
}

func (s *session) Poll(*imapserver.UpdateWriter, bool) error { return nil }
func (s *session) Idle(_ *imapserver.UpdateWriter, stop <-chan struct{}) error {
	<-stop
	return nil
}

func (s *session) Unselect() error {
	s.selected = ""
	return nil
}

func (s *session) Expunge(*imapserver.ExpungeWriter, *imap.UIDSet) error {
	return errors.New("expunge is not supported")
}

func (s *session) Search(_ imapserver.NumKind, _ *imap.SearchCriteria, _ *imap.SearchOptions) (*imap.SearchData, error) {
	return &imap.SearchData{}, nil
}

func (s *session) Fetch(w *imapserver.FetchWriter, numSet imap.NumSet, options *imap.FetchOptions) error {
	s.server.mu.Lock()
	defer s.server.mu.Unlock()
	box := s.mailboxLocked(s.selected)
	if box == nil {
		return errors.New("no mailbox selected")
	}
	uidSet, uidKind := numSet.(imap.UIDSet)
	seqSet, _ := numSet.(imap.SeqSet)
	for index, message := range box.messages {
		seqNum := uint32(index + 1)
		match := false
		if uidKind {
			match = uidSet.Contains(imap.UID(message.UID))
		} else {
			match = seqSet.Contains(seqNum)
		}
		if !match {
			continue
		}
		writer := w.CreateMessage(seqNum)
		if options.UID || uidKind {
			writer.WriteUID(imap.UID(message.UID))
		}
		if options.Envelope {
			writer.WriteEnvelope(&imap.Envelope{
				Date:      message.InternalDate,
				Subject:   message.Subject,
				From:      []imap.Address{{Mailbox: message.FromMailbox, Host: message.FromHost}},
				MessageID: message.MessageID,
			})
		}
		if options.InternalDate {
			writer.WriteInternalDate(message.InternalDate)
		}
		if options.RFC822Size {
			writer.WriteRFC822Size(int64(len(message.headerBlock()) + len(message.Body)))
		}
		if options.BodyStructure != nil {
			writer.WriteBodyStructure(message.bodyStructure())
		}
		for _, section := range options.BodySection {
			data := message.sectionData(section)
			sectionWriter := writer.WriteBodySection(section, int64(len(data)))
			_, writeErr := sectionWriter.Write(data)
			if writeErr == nil {
				writeErr = sectionWriter.Close()
			}
			if writeErr != nil {
				return writeErr
			}
		}
		if err := writer.Close(); err != nil {
			return err
		}
	}
	return nil
}

func (s *session) Store(*imapserver.FetchWriter, imap.NumSet, *imap.StoreFlags, *imap.StoreOptions) error {
	return errors.New("store is not supported in read-only tests")
}

func (s *session) Copy(imap.NumSet, string) (*imap.CopyData, error) {
	return nil, errors.New("copy is not supported")
}

// Move implements the atomic MOVE extension and reports COPYUID data so the
// client learns the destination UID.
func (s *session) Move(w *imapserver.MoveWriter, numSet imap.NumSet, dest string) error {
	s.server.mu.Lock()
	defer s.server.mu.Unlock()
	box := s.mailboxLocked(s.selected)
	if box == nil {
		return errors.New("no mailbox selected")
	}
	target := s.mailboxLocked(dest)
	if target == nil {
		target = s.server.newMailboxLocked()
		s.server.mailboxes[dest] = target
	}
	uidSet, ok := numSet.(imap.UIDSet)
	if !ok {
		return errors.New("only UID MOVE is supported")
	}
	var sourceUIDs, destUIDs imap.UIDSet
	var expunged []uint32
	kept := box.messages[:0]
	for index, message := range box.messages {
		if uidSet.Contains(imap.UID(message.UID)) {
			moved := *message
			moved.UID = target.nextUID
			target.nextUID++
			target.messages = append(target.messages, &moved)
			sourceUIDs.AddNum(imap.UID(message.UID))
			destUIDs.AddNum(imap.UID(moved.UID))
			expunged = append(expunged, uint32(index+1))
		} else {
			kept = append(kept, message)
		}
	}
	if len(expunged) == 0 {
		return errors.New("no matching messages")
	}
	box.messages = kept
	if err := w.WriteCopyData(&imap.CopyData{UIDValidity: target.uidValidity, SourceUIDs: sourceUIDs, DestUIDs: destUIDs}); err != nil {
		return err
	}
	for i := len(expunged) - 1; i >= 0; i-- {
		if err := w.WriteExpunge(expunged[i]); err != nil {
			return err
		}
	}
	return nil
}

func (m *Message) headerBlock() string {
	lines := []string{
		"From: " + m.FromMailbox + "@" + m.FromHost,
		"Subject: " + m.Subject,
		"Message-ID: <" + m.MessageID + ">",
		"Date: " + m.InternalDate.Format(time.RFC1123Z),
	}
	lines = append(lines, m.HeaderExtra...)
	return strings.Join(lines, "\r\n") + "\r\n\r\n"
}

func (m *Message) bodyStructure() *imap.BodyStructureSinglePart {
	return &imap.BodyStructureSinglePart{
		Type:     "text",
		Subtype:  "plain",
		Encoding: "7bit",
		Size:     uint32(len(m.Body)),
		Text:     &imap.BodyStructureText{NumLines: int64(strings.Count(m.Body, "\n") + 1)},
		Extended: &imap.BodyStructureSinglePartExt{},
	}
}

// sectionData renders the requested body section, honoring header field
// filters and partial ranges.
func (m *Message) sectionData(section *imap.FetchItemBodySection) []byte {
	var data []byte
	switch {
	case section.Specifier == imap.PartSpecifierHeader:
		data = filterHeaderLines(m.headerBlock(), section.HeaderFields)
	case section.Specifier == imap.PartSpecifierText:
		data = []byte(m.Body)
	case len(section.Part) > 0:
		// The test server only has a single text part at path [1].
		if len(section.Part) == 1 && section.Part[0] == 1 {
			data = []byte(m.Body)
		}
	default:
		data = []byte(m.headerBlock() + m.Body)
	}
	if section.Partial != nil {
		start := int(section.Partial.Offset)
		if start >= len(data) {
			return nil
		}
		end := start + int(section.Partial.Size)
		if end > len(data) {
			end = len(data)
		}
		data = data[start:end]
	}
	return data
}

func filterHeaderLines(block string, wanted []string) []byte {
	if len(wanted) == 0 {
		return []byte(block)
	}
	allowed := map[string]bool{}
	for _, name := range wanted {
		allowed[strings.ToLower(strings.TrimSpace(name))] = true
	}
	var out []string
	for _, line := range strings.Split(strings.TrimRight(block, "\r\n"), "\r\n") {
		if line == "" {
			continue
		}
		name := line
		if index := strings.IndexByte(line, ':'); index > 0 {
			name = strings.ToLower(strings.TrimSpace(line[:index]))
		}
		if allowed[name] {
			out = append(out, line)
		}
	}
	return []byte(strings.Join(out, "\r\n") + "\r\n\r\n")
}
