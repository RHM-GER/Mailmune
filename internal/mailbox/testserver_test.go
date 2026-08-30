package mailbox

import (
	"bytes"
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
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
)

// The test IMAP server runs on loopback with implicit TLS using a
// self-signed certificate that is only trusted by the injected client
// template. Certificate verification stays fully enabled.

func selfSignedTLS(t *testing.T) (*tls.Config, *tls.Config) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
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
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(parsed)
	serverConfig := &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12}
	clientTemplate := &tls.Config{RootCAs: pool}
	return serverConfig, clientTemplate
}

type testMessage struct {
	uid          uint32
	fromMailbox  string
	fromHost     string
	subject      string
	messageID    string
	internalDate time.Time
	headerExtra  []string
	body         string
}

type testMailbox struct {
	uidValidity uint32
	nextUID     uint32
	messages    []*testMessage
}

// testIMAP is a controlled in-process IMAP server for integration tests.
type testIMAP struct {
	username string
	password string

	mu           sync.Mutex
	nextValidity uint32
	mailboxes    map[string]*testMailbox
	listCalls    int

	addr           string
	clientTemplate *tls.Config
	server         *imapserver.Server
	debug          bytes.Buffer
}

func startTestIMAP(t *testing.T, caps imap.CapSet) *testIMAP {
	t.Helper()
	server := &testIMAP{username: "user", password: "pass", nextValidity: 100, mailboxes: map[string]*testMailbox{}}
	server.mailboxes["INBOX"] = server.newMailboxLocked()
	server.mailboxes["Sent"] = server.newMailboxLocked()
	server.mailboxes["AI_SPAM_FILTER"] = server.newMailboxLocked()

	serverConfig, clientTemplate := selfSignedTLS(t)
	server.clientTemplate = clientTemplate
	listener, err := tls.Listen("tcp", "127.0.0.1:0", serverConfig)
	if err != nil {
		t.Fatal(err)
	}
	server.addr = listener.Addr().String()
	server.server = imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return &testSession{server: server}, &imapserver.GreetingData{}, nil
		},
		Caps:        caps,
		DebugWriter: &server.debug,
	})
	go func() { _ = server.server.Serve(listener) }()
	t.Cleanup(func() {
		_ = server.server.Close()
		if t.Failed() {
			t.Logf("IMAP session log:\n%s", server.debug.String())
		}
	})
	return server
}

func (s *testIMAP) newMailboxLocked() *testMailbox {
	s.nextValidity++
	return &testMailbox{uidValidity: s.nextValidity, nextUID: 1}
}

func (s *testIMAP) addMessage(folder, from, subject, body string, when time.Time, headerExtra ...string) uint32 {
	mailboxName, address := splitAddress(from)
	s.mu.Lock()
	defer s.mu.Unlock()
	box := s.mailboxes[folder]
	if box == nil {
		box = s.newMailboxLocked()
		s.mailboxes[folder] = box
	}
	message := &testMessage{uid: box.nextUID, fromMailbox: mailboxName, fromHost: address, subject: subject, messageID: subject + "-" + time.Now().Format("150405.000") + "@test.invalid", internalDate: when, headerExtra: headerExtra, body: body}
	box.nextUID++
	box.messages = append(box.messages, message)
	return message.uid
}

// recreateMailbox simulates a UIDVALIDITY change: all UIDs become invalid.
func (s *testIMAP) recreateMailbox(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mailboxes[name] = s.newMailboxLocked()
}

func (s *testIMAP) snapshot(folder string) []*testMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	box := s.mailboxes[folder]
	if box == nil {
		return nil
	}
	return append([]*testMessage(nil), box.messages...)
}

func splitAddress(address string) (string, string) {
	parts := strings.SplitN(address, "@", 2)
	if len(parts) != 2 {
		return address, "invalid"
	}
	return parts[0], parts[1]
}

type testSession struct {
	server   *testIMAP
	selected string
}

var (
	_ imapserver.Session         = (*testSession)(nil)
	_ imapserver.SessionMove     = (*testSession)(nil)
	_ imapserver.SessionIMAP4rev2 = (*testSession)(nil)
)

func (s *testSession) Close() error { return nil }

func (s *testSession) Login(username, password string) error {
	if username != s.server.username || password != s.server.password {
		return imapserver.ErrAuthFailed
	}
	return nil
}

func (s *testSession) mailboxLocked(name string) *testMailbox {
	return s.server.mailboxes[name]
}

func (s *testSession) Select(mailbox string, _ *imap.SelectOptions) (*imap.SelectData, error) {
	s.server.mu.Lock()
	defer s.server.mu.Unlock()
	box := s.mailboxLocked(mailbox)
	if box == nil {
		return nil, errors.New("no such mailbox")
	}
	s.selected = mailbox
	return &imap.SelectData{
		Flags:         []imap.Flag{imap.FlagSeen, imap.FlagAnswered, imap.FlagFlagged},
		PermanentFlags: []imap.Flag{imap.FlagSeen, imap.FlagAnswered, imap.FlagFlagged},
		NumMessages:   uint32(len(box.messages)),
		UIDNext:       imap.UID(box.nextUID),
		UIDValidity:   box.uidValidity,
	}, nil
}

func (s *testSession) Create(mailbox string, _ *imap.CreateOptions) error {
	s.server.mu.Lock()
	defer s.server.mu.Unlock()
	if s.mailboxLocked(mailbox) != nil {
		return errors.New("mailbox exists")
	}
	s.server.mailboxes[mailbox] = s.server.newMailboxLocked()
	return nil
}

func (s *testSession) Delete(string) error                      { return errors.New("deletion is not supported") }
func (s *testSession) Rename(string, string, *imap.RenameOptions) error {
	return errors.New("rename is not supported")
}
func (s *testSession) Subscribe(string) error   { return nil }
func (s *testSession) Unsubscribe(string) error { return nil }

func (s *testSession) Namespace() (*imap.NamespaceData, error) {
	return &imap.NamespaceData{Personal: []imap.NamespaceDescriptor{{Prefix: "", Delim: '/'}}}, nil
}

func (s *testSession) List(w *imapserver.ListWriter, _ string, _ []string, _ *imap.ListOptions) error {
	s.server.mu.Lock()
	defer s.server.mu.Unlock()
	s.server.listCalls++
	for name := range s.server.mailboxes {
		if err := w.WriteList(&imap.ListData{Attrs: []imap.MailboxAttr{}, Delim: '/', Mailbox: name}); err != nil {
			return err
		}
	}
	return nil
}

func (s *testSession) Status(mailbox string, _ *imap.StatusOptions) (*imap.StatusData, error) {
	s.server.mu.Lock()
	defer s.server.mu.Unlock()
	box := s.mailboxLocked(mailbox)
	if box == nil {
		return nil, errors.New("no such mailbox")
	}
	numMessages := uint32(len(box.messages))
	return &imap.StatusData{Mailbox: mailbox, UIDValidity: box.uidValidity, UIDNext: imap.UID(box.nextUID), NumMessages: &numMessages}, nil
}

func (s *testSession) Append(string, imap.LiteralReader, *imap.AppendOptions) (*imap.AppendData, error) {
	return nil, errors.New("append is not supported")
}

func (s *testSession) Poll(*imapserver.UpdateWriter, bool) error { return nil }
func (s *testSession) Idle(_ *imapserver.UpdateWriter, stop <-chan struct{}) error {
	<-stop
	return nil
}

func (s *testSession) Unselect() error {
	s.selected = ""
	return nil
}

func (s *testSession) Expunge(*imapserver.ExpungeWriter, *imap.UIDSet) error {
	return errors.New("expunge is not supported")
}

func (s *testSession) Search(_ imapserver.NumKind, _ *imap.SearchCriteria, _ *imap.SearchOptions) (*imap.SearchData, error) {
	return &imap.SearchData{}, nil
}

func (s *testSession) Fetch(w *imapserver.FetchWriter, numSet imap.NumSet, options *imap.FetchOptions) error {
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
			match = uidSet.Contains(imap.UID(message.uid))
		} else {
			match = seqSet.Contains(seqNum)
		}
		if !match {
			continue
		}
		writer := w.CreateMessage(seqNum)
		if options.UID || uidKind {
			writer.WriteUID(imap.UID(message.uid))
		}
		if options.Envelope {
			writer.WriteEnvelope(&imap.Envelope{
				Date:      message.internalDate,
				Subject:   message.subject,
				From:      []imap.Address{{Mailbox: message.fromMailbox, Host: message.fromHost}},
				MessageID: message.messageID,
			})
		}
		if options.InternalDate {
			writer.WriteInternalDate(message.internalDate)
		}
		if options.RFC822Size {
			writer.WriteRFC822Size(int64(len(message.headerBlock()) + len(message.body)))
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

func (s *testSession) Store(*imapserver.FetchWriter, imap.NumSet, *imap.StoreFlags, *imap.StoreOptions) error {
	return errors.New("store is not supported in read-only tests")
}

func (s *testSession) Copy(imap.NumSet, string) (*imap.CopyData, error) {
	return nil, errors.New("copy is not supported")
}

// Move implements the atomic MOVE extension. It reports COPYUID data so the
// client learns the destination UID.
func (s *testSession) Move(w *imapserver.MoveWriter, numSet imap.NumSet, dest string) error {
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
		if uidSet.Contains(imap.UID(message.uid)) {
			moved := *message
			moved.uid = target.nextUID
			target.nextUID++
			target.messages = append(target.messages, &moved)
			sourceUIDs.AddNum(imap.UID(message.uid))
			destUIDs.AddNum(imap.UID(moved.uid))
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

func (m *testMessage) headerBlock() string {
	lines := []string{
		"From: " + m.fromMailbox + "@" + m.fromHost,
		"Subject: " + m.subject,
		"Message-ID: <" + m.messageID + ">",
		"Date: " + m.internalDate.Format(time.RFC1123Z),
	}
	lines = append(lines, m.headerExtra...)
	return strings.Join(lines, "\r\n") + "\r\n\r\n"
}

func (m *testMessage) bodyStructure() *imap.BodyStructureSinglePart {
	return &imap.BodyStructureSinglePart{
		Type:     "text",
		Subtype:  "plain",
		Encoding: "7bit",
		Size:     uint32(len(m.body)),
		Text:     &imap.BodyStructureText{NumLines: int64(strings.Count(m.body, "\n") + 1)},
		Extended: &imap.BodyStructureSinglePartExt{},
	}
}

// sectionData renders the requested body section, honoring header field
// filters and partial ranges.
func (m *testMessage) sectionData(section *imap.FetchItemBodySection) []byte {
	var data []byte
	switch {
	case section.Specifier == imap.PartSpecifierHeader:
		data = filterHeaderLines(m.headerBlock(), section.HeaderFields)
	case section.Specifier == imap.PartSpecifierText:
		data = []byte(m.body)
	case len(section.Part) > 0:
		// The test server only has a single text part at path [1].
		if len(section.Part) == 1 && section.Part[0] == 1 {
			data = []byte(m.body)
		}
	default:
		data = []byte(m.headerBlock() + m.body)
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
