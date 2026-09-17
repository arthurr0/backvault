package email

import (
	"bufio"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/notify/internal/testevent"
)

type fakeSMTP struct {
	listener net.Listener
	mu       sync.Mutex
	messages []string
	from     string
	rcpt     []string
	authOK   bool
}

func startSMTP(t *testing.T) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeSMTP{listener: ln}
	go s.accept()
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

func (s *fakeSMTP) addr() (string, int) {
	host, port, _ := net.SplitHostPort(s.listener.Addr().String())
	p := 0
	for _, c := range port {
		p = p*10 + int(c-'0')
	}
	return host, p
}

func (s *fakeSMTP) accept() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *fakeSMTP) handle(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	write := func(line string) { _, _ = conn.Write([]byte(line + "\r\n")) }
	write("220 fake.smtp.test ESMTP Backvault test server")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"):
			write("250-fake.smtp.test")
			write("250-AUTH PLAIN LOGIN")
			write("250 8BITMIME")
		case strings.HasPrefix(cmd, "HELO"):
			write("250 fake.smtp.test")
		case strings.HasPrefix(cmd, "AUTH"):
			s.mu.Lock()
			s.authOK = true
			s.mu.Unlock()
			write("235 2.7.0 Authentication successful")
		case strings.HasPrefix(cmd, "MAIL FROM"):
			s.mu.Lock()
			s.from = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line)[10:], ":"))
			s.mu.Unlock()
			write("250 2.1.0 Ok")
		case strings.HasPrefix(cmd, "RCPT TO"):
			s.mu.Lock()
			s.rcpt = append(s.rcpt, strings.TrimSpace(strings.TrimSpace(line)[8:]))
			s.mu.Unlock()
			write("250 2.1.5 Ok")
		case cmd == "DATA":
			write("354 End data with <CR><LF>.<CR><LF>")
			var body strings.Builder
			for {
				dataLine, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimRight(dataLine, "\r\n") == "." {
					break
				}
				body.WriteString(dataLine)
			}
			s.mu.Lock()
			s.messages = append(s.messages, body.String())
			s.mu.Unlock()
			write("250 2.0.0 Ok: queued")
		case cmd == "QUIT":
			write("221 2.0.0 Bye")
			return
		case cmd == "RSET":
			write("250 2.0.0 Ok")
		default:
			write("502 5.5.2 Command not implemented")
		}
	}
}

func (s *fakeSMTP) last() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.messages) == 0 {
		return ""
	}
	return s.messages[len(s.messages)-1]
}

func TestValidate(t *testing.T) {
	d := New()
	if err := d.Validate(core.Config{}); err == nil {
		t.Error("host, from and to should be required")
	}
	base := core.Config{"smtp_host": "smtp.example.com", "from": "Backvault <c@example.com>", "to": []string{"ops@example.com"}}
	if err := d.Validate(base); err != nil {
		t.Errorf("valid config rejected: %v", err)
	}
	bad := base.Clone()
	bad["from"] = "not an address"
	if err := d.Validate(bad); err == nil {
		t.Error("invalid from address should be rejected")
	}
	bad = base.Clone()
	bad["to"] = []string{"nope"}
	if err := d.Validate(bad); err == nil {
		t.Error("invalid recipient should be rejected")
	}
	bad = base.Clone()
	bad["tls_mode"] = "quantum"
	if err := d.Validate(bad); err == nil {
		t.Error("unknown tls mode should be rejected")
	}
}

func TestSendDeliversMultipartMessage(t *testing.T) {
	srv := startSMTP(t)
	host, port := srv.addr()
	cfg := core.Config{
		"smtp_host": host,
		"smtp_port": port,
		"tls_mode":  "none",
		"from":      "Backvault <backups@example.com>",
		"to":        []string{"ops@example.com", "oncall@example.com"},
	}
	if err := New().Send(t.Context(), cfg, testevent.Failed(), testevent.Logger()); err != nil {
		t.Fatalf("send: %v", err)
	}
	msg := srv.last()
	if msg == "" {
		t.Fatal("no message was delivered")
	}
	if !strings.Contains(msg, "multipart/alternative") {
		t.Error("message is not multipart/alternative")
	}
	if !strings.Contains(msg, "text/plain") || !strings.Contains(msg, "text/html") {
		t.Error("message is missing one of the two parts")
	}
	if !strings.Contains(msg, "To: ops@example.com, oncall@example.com") {
		t.Errorf("recipients header is wrong:\n%s", msg)
	}
	for _, want := range []string{"Acme Backups", "Database nightly", "failed", "Hetzner Box, MinIO", "could not connect", "https://backvault.example.com/runs/run1"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message is missing %q", want)
		}
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if !strings.HasPrefix(srv.from, "<backups@example.com>") {
		t.Errorf("MAIL FROM %q", srv.from)
	}
	if len(srv.rcpt) != 2 {
		t.Errorf("RCPT TO %v", srv.rcpt)
	}
}

func TestSubjectPrefixAndStatus(t *testing.T) {
	srv := startSMTP(t)
	host, port := srv.addr()
	cfg := core.Config{
		"smtp_host": host, "smtp_port": port, "tls_mode": "none",
		"from": "backups@example.com", "to": []string{"ops@example.com"},
		"subject_prefix": "[Backvault]",
	}
	if err := New().Send(t.Context(), cfg, testevent.Success(), testevent.Logger()); err != nil {
		t.Fatalf("send: %v", err)
	}
	msg := srv.last()
	if !strings.Contains(msg, "Subject: [Backvault]") {
		t.Errorf("subject prefix missing:\n%s", msg)
	}
	if !strings.Contains(msg, "Website files") {
		t.Error("subject does not name the job")
	}
}

func TestAuthenticationIsPerformed(t *testing.T) {
	srv := startSMTP(t)
	host, port := srv.addr()
	cfg := core.Config{
		"smtp_host": host, "smtp_port": port, "tls_mode": "none",
		"from": "backups@example.com", "to": []string{"ops@example.com"},
		"username": "backvault", "password": "secret", "allow_insecure_auth": true,
	}
	if err := New().Send(t.Context(), cfg, testevent.Success(), testevent.Logger()); err != nil {
		t.Fatalf("send: %v", err)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if !srv.authOK {
		t.Error("the server never saw an AUTH command")
	}
}

func TestPasswordNeverAppearsInTheMessage(t *testing.T) {
	srv := startSMTP(t)
	host, port := srv.addr()
	cfg := core.Config{
		"smtp_host": host, "smtp_port": port, "tls_mode": "none",
		"from": "backups@example.com", "to": []string{"ops@example.com"},
		"username": "backvault", "password": "hunter2hunter2", "allow_insecure_auth": true,
	}
	if err := New().Send(t.Context(), cfg, testevent.Failed(), testevent.Logger()); err != nil {
		t.Fatalf("send: %v", err)
	}
	if strings.Contains(srv.last(), "hunter2hunter2") {
		t.Error("the smtp password leaked into the message body")
	}
}

func TestSendReportsConnectionFailure(t *testing.T) {
	cfg := core.Config{
		"smtp_host": "127.0.0.1", "smtp_port": 1, "tls_mode": "none",
		"from": "backups@example.com", "to": []string{"ops@example.com"}, "timeout": 2,
	}
	if err := New().Send(t.Context(), cfg, testevent.Success(), testevent.Logger()); err == nil {
		t.Error("connecting to a closed port should fail")
	}
}
