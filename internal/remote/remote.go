package remote

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/arthurr0/backvault/internal/core"

	cryptossh "golang.org/x/crypto/ssh"
)

const (
	DefaultPort           = 22
	DefaultConnectTimeout = 15 * time.Second
	tailLines             = 20
	stderrMaxLine         = 64 * 1024
)

var KnownTools = []string{
	"tar", "docker", "podman", "pg_dump", "pg_dumpall", "pg_restore", "psql",
	"mysqldump", "mariadb-dump", "mysql", "mariadb", "mongodump", "mongorestore",
	"redis-cli", "sqlite3", "zstd", "gzip", "sudo",
}

func Validate(h core.Host) error {
	var problems []string
	if strings.TrimSpace(h.Address) == "" {
		problems = append(problems, "address is required")
	}
	if h.Port < 0 || h.Port > 65535 {
		problems = append(problems, "port must be between 1 and 65535")
	}
	if strings.TrimSpace(h.User) == "" {
		problems = append(problems, "user is required")
	}
	switch h.Auth {
	case core.HostAuthKey, "":
		if strings.TrimSpace(h.PrivateKey) == "" {
			problems = append(problems, "a private key is required for key authentication")
		} else if _, err := parseSigner(h); err != nil {
			problems = append(problems, err.Error())
		}
	case core.HostAuthPassword:
		if h.Password == "" {
			problems = append(problems, "a password is required for password authentication")
		}
	default:
		problems = append(problems, "auth must be key or password")
	}
	if hk := strings.TrimSpace(h.HostKey); hk != "" && !strings.HasPrefix(hk, "SHA256:") {
		problems = append(problems, "host key fingerprint must look like SHA256:abc...")
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func GenerateKey(comment string) (privatePEM, publicLine string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("generate key: %w", err)
	}
	block, err := cryptossh.MarshalPrivateKey(priv, comment)
	if err != nil {
		return "", "", fmt.Errorf("encode private key: %w", err)
	}
	sshPub, err := cryptossh.NewPublicKey(pub)
	if err != nil {
		return "", "", fmt.Errorf("encode public key: %w", err)
	}
	line := strings.TrimSpace(string(cryptossh.MarshalAuthorizedKey(sshPub)))
	if comment != "" {
		line += " " + comment
	}
	return string(pemEncode(block)), line, nil
}

func PublicKeyOf(h core.Host) (string, error) {
	signer, err := parseSigner(h)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(cryptossh.MarshalAuthorizedKey(signer.PublicKey()))), nil
}

func parseSigner(h core.Host) (cryptossh.Signer, error) {
	pem := strings.TrimSpace(h.PrivateKey)
	if pem == "" {
		return nil, errors.New("private key is empty")
	}
	var signer cryptossh.Signer
	var err error
	if h.KeyPassphrase != "" {
		signer, err = cryptossh.ParsePrivateKeyWithPassphrase([]byte(pem), []byte(h.KeyPassphrase))
	} else {
		signer, err = cryptossh.ParsePrivateKey([]byte(pem))
	}
	if err != nil {
		var missing *cryptossh.PassphraseMissingError
		if errors.As(err, &missing) {
			return nil, errors.New("the private key is encrypted, set the key passphrase")
		}
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	return signer, nil
}

func authMethods(h core.Host) ([]cryptossh.AuthMethod, error) {
	switch h.Auth {
	case core.HostAuthPassword:
		pw := h.Password
		if pw == "" {
			return nil, errors.New("password authentication selected but no password is set")
		}
		return []cryptossh.AuthMethod{
			cryptossh.Password(pw),
			cryptossh.KeyboardInteractive(func(name, instruction string, questions []string, echos []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range answers {
					answers[i] = pw
				}
				return answers, nil
			}),
		}, nil
	default:
		signer, err := parseSigner(h)
		if err != nil {
			return nil, err
		}
		return []cryptossh.AuthMethod{cryptossh.PublicKeys(signer)}, nil
	}
}

func hostKeyCallback(h core.Host, log *slog.Logger) (cryptossh.HostKeyCallback, error) {
	expected := strings.TrimSpace(h.HostKey)
	if expected == "" {
		return func(hostname string, addr net.Addr, key cryptossh.PublicKey) error {
			if log != nil {
				log.Warn("accepting unverified ssh host key, copy it into the host key field to pin it",
					"host", hostname, "type", key.Type(), "fingerprint", cryptossh.FingerprintSHA256(key))
			}
			return nil
		}, nil
	}
	if !strings.HasPrefix(expected, "SHA256:") {
		return nil, errors.New("host key fingerprint must look like SHA256:abc...")
	}
	return func(hostname string, addr net.Addr, key cryptossh.PublicKey) error {
		got := cryptossh.FingerprintSHA256(key)
		if got != expected {
			return fmt.Errorf("host key mismatch for %s: expected %s, got %s", hostname, expected, got)
		}
		return nil
	}, nil
}

func Scrub(h core.Host, err error) error {
	if err == nil {
		return nil
	}
	msg := ScrubText(h, err.Error())
	if msg == err.Error() {
		return err
	}
	return errors.New(msg)
}

func ScrubText(h core.Host, text string) string {
	for _, secret := range []string{h.Password, h.KeyPassphrase, h.PrivateKey} {
		if len(secret) >= 3 {
			text = strings.ReplaceAll(text, secret, "***")
		}
	}
	return text
}

type Client struct {
	host   core.Host
	ssh    *cryptossh.Client
	log    *slog.Logger
	redact []string
}

func (c *Client) WithRedact(values ...string) *Client {
	out := *c
	out.redact = append(append([]string{}, c.redact...), values...)
	return &out
}

func (c *Client) scrub(text string) string {
	text = ScrubText(c.host, text)
	for _, v := range c.redact {
		if len(v) >= 3 {
			text = strings.ReplaceAll(text, v, "***")
		}
	}
	return text
}

func Dial(ctx context.Context, h core.Host, log *slog.Logger) (*Client, error) {
	if log == nil {
		log = slog.Default()
	}
	timeout := time.Duration(h.ConnectTimeout) * time.Second
	if timeout <= 0 {
		timeout = DefaultConnectTimeout
	}
	auths, err := authMethods(h)
	if err != nil {
		return nil, err
	}
	hostKey, err := hostKeyCallback(h, log)
	if err != nil {
		return nil, err
	}
	port := h.Port
	if port <= 0 {
		port = DefaultPort
	}
	addr := net.JoinHostPort(strings.TrimSpace(h.Address), strconv.Itoa(port))
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("ssh connect to %s: %w", addr, err)
	}
	clientCfg := &cryptossh.ClientConfig{User: h.User, Auth: auths, HostKeyCallback: hostKey, Timeout: timeout}
	type result struct {
		c   *cryptossh.Client
		err error
	}
	done := make(chan result, 1)
	go func() {
		sshConn, chans, reqs, err := cryptossh.NewClientConn(conn, addr, clientCfg)
		if err != nil {
			done <- result{err: err}
			return
		}
		done <- result{c: cryptossh.NewClient(sshConn, chans, reqs)}
	}()
	select {
	case <-ctx.Done():
		conn.Close()
		return nil, ctx.Err()
	case r := <-done:
		if r.err != nil {
			conn.Close()
			return nil, fmt.Errorf("ssh connect to %s: %w", addr, Scrub(h, r.err))
		}
		return &Client{host: h, ssh: r.c, log: log}, nil
	}
}

func (c *Client) Host() core.Host { return c.host }

func (c *Client) Close() error { return c.ssh.Close() }

func (c *Client) Command(cmd string) string { return Wrap(c.host, cmd) }

func Wrap(h core.Host, cmd string) string {
	if h.Sudo {
		return "sudo -n -- sh -c " + ShellQuote(cmd)
	}
	return cmd
}

type Output struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

func (c *Client) Exec(ctx context.Context, cmd string, stdin io.Reader) (Output, error) {
	sess, err := c.ssh.NewSession()
	if err != nil {
		return Output{}, fmt.Errorf("open ssh session: %w", err)
	}
	defer sess.Close()
	var stdout, stderr bytes.Buffer
	sess.Stdout = &stdout
	sess.Stderr = &stderr
	if stdin != nil {
		sess.Stdin = stdin
	}
	done := make(chan error, 1)
	go func() { done <- sess.Run(c.Command(cmd)) }()
	select {
	case <-ctx.Done():
		_ = sess.Signal(cryptossh.SIGTERM)
		_ = sess.Close()
		return Output{Stdout: stdout.String(), Stderr: stderr.String()}, ctx.Err()
	case err := <-done:
		out := Output{Stdout: stdout.String(), Stderr: c.scrub(stderr.String())}
		var exitErr *cryptossh.ExitError
		if errors.As(err, &exitErr) {
			out.ExitCode = exitErr.ExitStatus()
			return out, nil
		}
		if err != nil {
			return out, errors.New(c.scrub(err.Error()))
		}
		return out, nil
	}
}

type Stream struct {
	client *cryptossh.Client
	sess   *cryptossh.Session
	stdout io.Reader
	host   core.Host
	label  string
	wg     sync.WaitGroup
	done   chan struct{}
	tailMu sync.Mutex
	tail   []string
	once   sync.Once
	err    error
	eof    bool
	owns   bool
	scrub  func(string) string
}

func (c *Client) Start(ctx context.Context, cmd string, stdin io.Reader, label string) (*Stream, error) {
	return start(ctx, c, cmd, stdin, label, false)
}

func Run(ctx context.Context, h core.Host, cmd string, stdin io.Reader, label string, log *slog.Logger) (*Stream, error) {
	c, err := Dial(ctx, h, log)
	if err != nil {
		return nil, err
	}
	st, err := start(ctx, c, cmd, stdin, label, true)
	if err != nil {
		c.Close()
		return nil, err
	}
	return st, nil
}

func start(ctx context.Context, c *Client, cmd string, stdin io.Reader, label string, owns bool) (*Stream, error) {
	sess, err := c.ssh.NewSession()
	if err != nil {
		return nil, fmt.Errorf("open ssh session: %w", err)
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		sess.Close()
		return nil, fmt.Errorf("remote stdout: %w", err)
	}
	stderr, err := sess.StderrPipe()
	if err != nil {
		sess.Close()
		return nil, fmt.Errorf("remote stderr: %w", err)
	}
	if stdin != nil {
		sess.Stdin = stdin
	}
	if label == "" {
		label = c.host.Name
	}
	st := &Stream{client: c.ssh, sess: sess, stdout: stdout, host: c.host, label: label, done: make(chan struct{}), owns: owns, scrub: c.scrub}
	st.wg.Add(1)
	go func() {
		defer st.wg.Done()
		lines := bufio.NewScanner(stderr)
		lines.Buffer(make([]byte, 0, 8192), stderrMaxLine)
		for lines.Scan() {
			line := strings.TrimRight(lines.Text(), "\r")
			if strings.TrimSpace(line) == "" {
				continue
			}
			line = c.scrub(line)
			st.addTail(line)
			if looksLikeError(line) {
				c.log.Warn(line, "host", label)
			} else {
				c.log.Info(line, "host", label)
			}
		}
	}()
	go func() {
		select {
		case <-ctx.Done():
			_ = sess.Signal(cryptossh.SIGTERM)
			_ = sess.Close()
		case <-st.done:
		}
	}()
	if err := sess.Start(c.Command(cmd)); err != nil {
		close(st.done)
		sess.Close()
		return nil, errors.New("start remote command: " + c.scrub(err.Error()))
	}
	return st, nil
}

func looksLikeError(line string) bool {
	l := strings.ToLower(line)
	for _, marker := range []string{"error", "fatal", "denied", "failed", "cannot", "no such", "not found", "refused"} {
		if strings.Contains(l, marker) {
			return true
		}
	}
	return false
}

func (s *Stream) addTail(line string) {
	s.tailMu.Lock()
	defer s.tailMu.Unlock()
	s.tail = append(s.tail, line)
	if len(s.tail) > tailLines {
		s.tail = s.tail[len(s.tail)-tailLines:]
	}
}

func (s *Stream) Tail() string {
	s.tailMu.Lock()
	defer s.tailMu.Unlock()
	return strings.Join(s.tail, " | ")
}

func (s *Stream) Read(b []byte) (int, error) {
	n, err := s.stdout.Read(b)
	if errors.Is(err, io.EOF) {
		s.eof = true
	}
	return n, err
}

func (s *Stream) Close() error {
	s.once.Do(func() {
		aborted := !s.eof
		if aborted {
			_ = s.sess.Signal(cryptossh.SIGTERM)
			_ = s.sess.Close()
		}
		s.wg.Wait()
		waitErr := s.sess.Wait()
		close(s.done)
		_ = s.sess.Close()
		if s.owns {
			_ = s.client.Close()
		}
		switch {
		case aborted:
			s.err = errors.New("remote command was stopped before it finished")
		case waitErr != nil:
			detail := s.Tail()
			var exitErr *cryptossh.ExitError
			if errors.As(waitErr, &exitErr) {
				if detail != "" {
					s.err = fmt.Errorf("remote command exited with code %d: %s", exitErr.ExitStatus(), detail)
				} else {
					s.err = fmt.Errorf("remote command exited with code %d", exitErr.ExitStatus())
				}
				return
			}
			if detail != "" {
				s.err = errors.New("remote command failed: " + s.scrub(waitErr.Error()) + ": " + detail)
			} else {
				s.err = errors.New("remote command failed: " + s.scrub(waitErr.Error()))
			}
		}
	})
	return s.err
}

type Probe struct {
	OS    string
	Tools []string
}

func (c *Client) Probe(ctx context.Context) (Probe, error) {
	var p Probe
	out, err := c.Exec(ctx, "uname -srm 2>/dev/null || ver", nil)
	if err != nil {
		return p, err
	}
	if out.ExitCode != 0 {
		return p, fmt.Errorf("remote shell check exited with code %d: %s", out.ExitCode, strings.TrimSpace(out.Stderr))
	}
	p.OS = strings.TrimSpace(out.Stdout)
	var b strings.Builder
	for _, tool := range KnownTools {
		b.WriteString("command -v " + tool + " >/dev/null 2>&1 && echo " + tool + "; ")
	}
	b.WriteString("true")
	out, err = c.Exec(ctx, b.String(), nil)
	if err != nil {
		return p, err
	}
	for _, line := range strings.Split(out.Stdout, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			p.Tools = append(p.Tools, t)
		}
	}
	return p, nil
}

func ShellQuote(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_./:=@,+", r)) {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func ShellJoin(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, a := range args {
		quoted = append(quoted, ShellQuote(a))
	}
	return strings.Join(quoted, " ")
}
