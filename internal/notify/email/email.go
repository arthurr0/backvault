package email

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/notify"
	"github.com/arthurr0/backvault/internal/notify/internal/message"
)

type Driver struct{}

func New() *Driver { return &Driver{} }

func init() { notify.Register(New()) }

func (d *Driver) Spec() core.DriverSpec {
	return core.DriverSpec{
		Kind:         "email",
		Label:        "Email",
		Description:  "Sends run notifications over SMTP as a formatted HTML message with a plain text fallback.",
		Icon:         "mail",
		Category:     "Email",
		Capabilities: []string{core.CapTest},
		Fields: []core.Field{
			{
				Name: "smtp_host", Label: "SMTP host", Type: core.FieldString, Required: true,
				Group: "Server", Placeholder: "smtp.example.com",
			},
			{
				Name: "smtp_port", Label: "SMTP port", Type: core.FieldPort, Default: 587, Group: "Server",
				Help: "587 for STARTTLS, 465 for implicit TLS, 25 for an unencrypted relay.",
			},
			{
				Name: "tls_mode", Label: "Encryption", Type: core.FieldSelect, Default: "starttls",
				Group: "Server",
				Options: []core.FieldOption{
					{Value: "starttls", Label: "STARTTLS (port 587)"},
					{Value: "tls", Label: "Implicit TLS (port 465)"},
					{Value: "none", Label: "None (plain, port 25)"},
				},
			},
			{
				Name: "username", Label: "Username", Type: core.FieldString, Group: "Server",
				Placeholder: "backvault@example.com",
				Help:        "Leave empty for a relay that does not require authentication.",
			},
			{
				Name: "password", Label: "Password", Type: core.FieldSecret, Secret: true, Group: "Server",
			},
			{
				Name: "from", Label: "From", Type: core.FieldString, Required: true, Group: "Message",
				Placeholder: "Backvault <backups@example.com>",
			},
			{
				Name: "to", Label: "To", Type: core.FieldStringList, Required: true, Group: "Message",
				Placeholder: "ops@example.com",
				Help:        "One recipient per line.",
			},
			{
				Name: "subject_prefix", Label: "Subject prefix", Type: core.FieldString, Group: "Message",
				Advanced: true, Placeholder: "[Backvault]",
				Help: "Optional text put in front of every subject line.",
			},
			{
				Name: "skip_tls_verify", Label: "Skip certificate verification", Type: core.FieldBool,
				Default: false, Group: "Advanced", Advanced: true,
			},
			{
				Name: "allow_insecure_auth", Label: "Allow password over a plain connection", Type: core.FieldBool,
				Default: false, Group: "Advanced", Advanced: true,
				Help: "Only for a relay on localhost or a trusted private network.",
			},
			{
				Name: "helo_name", Label: "HELO name", Type: core.FieldString, Group: "Advanced",
				Advanced: true, Placeholder: "backvault.example.com",
				Help: "Name announced to the SMTP server. Defaults to the hostname of this machine.",
			},
			{
				Name: "timeout", Label: "Timeout (seconds)", Type: core.FieldInt, Default: 30,
				Group: "Advanced", Advanced: true,
			},
		},
	}
}

func (d *Driver) Validate(cfg core.Config) error {
	if err := core.ValidateRequired(d.Spec(), cfg); err != nil {
		return err
	}
	if _, err := mail.ParseAddress(cfg.String("from")); err != nil {
		return fmt.Errorf("from address is not valid")
	}
	recipients := cfg.StringList("to")
	if len(recipients) == 0 {
		return fmt.Errorf("at least one recipient is required")
	}
	for _, r := range recipients {
		if _, err := mail.ParseAddress(r); err != nil {
			return fmt.Errorf("recipient %q is not a valid address", r)
		}
	}
	switch cfg.StringOr("tls_mode", "starttls") {
	case "starttls", "tls", "none":
	default:
		return fmt.Errorf("encryption must be starttls, tls or none")
	}
	return nil
}

func (d *Driver) Send(ctx context.Context, cfg core.Config, ev notify.Event, log *slog.Logger) error {
	if err := d.Validate(cfg); err != nil {
		return err
	}
	m := message.Build(ev)
	subject := m.Subject()
	if prefix := strings.TrimSpace(cfg.String("subject_prefix")); prefix != "" {
		subject = prefix + " " + subject
	}
	from, err := mail.ParseAddress(cfg.String("from"))
	if err != nil {
		return fmt.Errorf("from address is not valid: %w", err)
	}
	recipients := cfg.StringList("to")
	addrs := make([]string, 0, len(recipients))
	for _, r := range recipients {
		parsed, err := mail.ParseAddress(r)
		if err != nil {
			return fmt.Errorf("recipient %q is not a valid address: %w", r, err)
		}
		addrs = append(addrs, parsed.Address)
	}
	body, err := build(from.String(), recipients, subject, m)
	if err != nil {
		return err
	}
	if err := deliver(ctx, cfg, from.Address, addrs, body); err != nil {
		return err
	}
	log.Info("notification email sent", "recipients", len(addrs), "subject", subject)
	return nil
}

func build(from string, to []string, subject string, m message.Model) ([]byte, error) {
	var b strings.Builder
	boundary, err := randomBoundary()
	if err != nil {
		return nil, err
	}
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + strings.Join(to, ", ") + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", subject) + "\r\n")
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("Message-ID: <" + messageID(from) + ">\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("X-Backvault-Event: " + m.Status + "\r\n")
	b.WriteString("Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n\r\n")

	writer := multipart.NewWriter(&b)
	if err := writer.SetBoundary(boundary); err != nil {
		return nil, fmt.Errorf("set mime boundary: %w", err)
	}
	text, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {"text/plain; charset=utf-8"},
		"Content-Transfer-Encoding": {"8bit"},
	})
	if err != nil {
		return nil, fmt.Errorf("build text part: %w", err)
	}
	if _, err := text.Write([]byte(normalize(m.Text()))); err != nil {
		return nil, fmt.Errorf("write text part: %w", err)
	}
	htmlPart, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {"text/html; charset=utf-8"},
		"Content-Transfer-Encoding": {"8bit"},
	})
	if err != nil {
		return nil, fmt.Errorf("build html part: %w", err)
	}
	if _, err := htmlPart.Write([]byte(normalize(m.HTML()))); err != nil {
		return nil, fmt.Errorf("write html part: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close mime writer: %w", err)
	}
	return []byte(b.String()), nil
}

func normalize(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\n", "\r\n")
}

func messageID(from string) string {
	domain := "backvault.local"
	if i := strings.LastIndex(from, "@"); i >= 0 {
		domain = strings.Trim(from[i+1:], "> ")
	}
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d@%s", time.Now().UnixNano(), domain)
	}
	return hex.EncodeToString(buf) + "." + strconv.FormatInt(time.Now().Unix(), 36) + "@" + domain
}

func randomBoundary() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate mime boundary: %w", err)
	}
	return "backvault-" + hex.EncodeToString(buf), nil
}

func deliver(ctx context.Context, cfg core.Config, from string, to []string, body []byte) error {
	host := cfg.String("smtp_host")
	port := cfg.Int("smtp_port", 587)
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	timeout := time.Duration(cfg.Int("timeout", 30)) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	sendCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	tlsCfg := &tls.Config{ServerName: host, InsecureSkipVerify: cfg.Bool("skip_tls_verify", false)}
	dialer := &net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(sendCtx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("connect to smtp server %s: %w", addr, err)
	}
	mode := cfg.StringOr("tls_mode", "starttls")
	if mode == "tls" {
		conn = tls.Client(conn, tlsCfg)
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-sendCtx.Done():
			_ = conn.Close()
		case <-stop:
		}
	}()

	c, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp handshake with %s: %w", addr, err)
	}
	defer c.Close()
	if err := c.Hello(helo(cfg)); err != nil {
		return fmt.Errorf("smtp greeting: %w", err)
	}
	if mode == "starttls" {
		ok, _ := c.Extension("STARTTLS")
		if !ok {
			return errors.New("the smtp server does not offer STARTTLS, choose another encryption mode")
		}
		if err := c.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("start tls: %w", err)
		}
	}
	if user := cfg.String("username"); user != "" {
		auth := smtp.PlainAuth("", user, cfg.String("password"), host)
		if _, isTLS := c.TLSConnectionState(); !isTLS && cfg.Bool("allow_insecure_auth", false) {
			auth = insecurePlain{identity: "", username: user, password: cfg.String("password"), host: host}
		}
		if ok, _ := c.Extension("AUTH"); ok {
			if err := c.Auth(auth); err != nil {
				return fmt.Errorf("smtp authentication failed for %s: %w", user, err)
			}
		}
	}
	if err := c.Mail(from); err != nil {
		return fmt.Errorf("smtp MAIL FROM: %w", err)
	}
	for _, rcpt := range to {
		if err := c.Rcpt(rcpt); err != nil {
			return fmt.Errorf("smtp RCPT TO %s: %w", rcpt, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp DATA: %w", err)
	}
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("write message: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("finish message: %w", err)
	}
	return c.Quit()
}

func helo(cfg core.Config) string {
	if h := strings.TrimSpace(cfg.String("helo_name")); h != "" {
		return h
	}
	if name, err := os.Hostname(); err == nil && name != "" {
		return name
	}
	return "localhost"
}

type insecurePlain struct {
	identity string
	username string
	password string
	host     string
}

func (a insecurePlain) Start(server *smtp.ServerInfo) (string, []byte, error) {
	if server.Name != a.host {
		return "", nil, errors.New("smtp server name does not match the configured host")
	}
	return "PLAIN", []byte(a.identity + "\x00" + a.username + "\x00" + a.password), nil
}

func (a insecurePlain) Next(fromServer []byte, more bool) ([]byte, error) {
	if more {
		return nil, errors.New("unexpected server challenge")
	}
	return nil, nil
}

var _ notify.Driver = (*Driver)(nil)
