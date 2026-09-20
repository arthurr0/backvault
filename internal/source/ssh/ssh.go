package ssh

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/source"
	"github.com/arthurr0/backvault/internal/source/internal/remoteexec"

	cryptossh "golang.org/x/crypto/ssh"
)

const tailLines = 20

type Driver struct{}

func New() *Driver { return &Driver{} }

func init() { source.Register(New()) }

func (d *Driver) Spec() core.DriverSpec {
	return core.DriverSpec{
		Kind:        "ssh",
		Label:       "Remote command over SSH",
		Description: "Runs a command on a remote host over SSH and streams its standard output straight into the backup.",
		Icon:        "terminal",
		Category:    "Remote",
		Capabilities: []string{
			core.CapTest,
			core.CapRestore,
			core.CapRemote,
		},
		Fields: []core.Field{
			{
				Name: "host", Label: "Host", Type: core.FieldString, Required: true, LocalOnly: true,
				Group: "Connection", Placeholder: "db01.example.com",
			},
			{
				Name: "port", Label: "Port", Type: core.FieldPort, Default: 22, Group: "Connection",
				LocalOnly: true,
			},
			{
				Name: "user", Label: "User", Type: core.FieldString, Required: true, Default: "root",
				Group: "Connection", Placeholder: "root", LocalOnly: true,
			},
			{
				Name: "auth", Label: "Authentication", Type: core.FieldSelect, Default: "key",
				Group: "Connection", LocalOnly: true,
				Options: []core.FieldOption{
					{Value: "key", Label: "Private key"},
					{Value: "password", Label: "Password"},
				},
			},
			{
				Name: "password", Label: "Password", Type: core.FieldSecret, Secret: true, LocalOnly: true,
				Group: "Connection", ShowIf: map[string]any{"auth": "password"},
			},
			{
				Name: "private_key", Label: "Private key", Type: core.FieldText, Secret: true, LocalOnly: true,
				Group: "Connection", ShowIf: map[string]any{"auth": "key"},
				Placeholder: "-----BEGIN OPENSSH PRIVATE KEY-----",
				Help:        "PEM text of the key. OpenSSH, RSA, ECDSA and Ed25519 keys are supported.",
			},
			{
				Name: "key_passphrase", Label: "Key passphrase", Type: core.FieldSecret, Secret: true, LocalOnly: true,
				Group: "Connection", ShowIf: map[string]any{"auth": "key"},
				Help: "Leave empty for an unencrypted key.",
			},
			{
				Name: "host_key", Label: "Host key fingerprint", Type: core.FieldString, LocalOnly: true,
				Group: "Connection", Advanced: true, Placeholder: "SHA256:xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
				Help: "Leave empty to trust the key on first use. The fingerprint is then written to the run log as a warning so you can pin it here.",
			},
			{
				Name: "command", Label: "Command", Type: core.FieldText, Required: true,
				Group: "Backup", Placeholder: "tar -C /var/www -cf - .",
				Help: "Must write the backup to standard output and exit with code 0. Examples: tar -C /var/www -cf - . or pg_dump -Fc app.",
			},
			{
				Name: "extension", Label: "File extension", Type: core.FieldString, Default: "tar",
				Group: "Backup", Placeholder: "tar",
				Help: "Used in the artifact filename. Use tar for tar output, dump for pg_dump custom format, sql for plain SQL.",
			},
			{
				Name: "restore_command", Label: "Restore command", Type: core.FieldText,
				Group: "Backup", Advanced: true, Placeholder: "tar -C /var/www -xf -",
				Help: "Optional. Runs on the same host with the unpacked artifact on standard input.",
			},
			{
				Name: "connect_timeout", Label: "Connect timeout (seconds)", Type: core.FieldInt, Default: 15,
				Group: "Advanced", Advanced: true, LocalOnly: true,
			},
		},
	}
}

func (d *Driver) Validate(cfg core.Config) error {
	if err := remoteexec.ValidateRequired(d.Spec(), cfg); err != nil {
		return err
	}
	if cfg.Host() != nil {
		if strings.TrimSpace(cfg.String("command")) == "" {
			return fmt.Errorf("command is required")
		}
		return nil
	}
	switch cfg.StringOr("auth", "key") {
	case "key":
		if !cfg.Has("private_key") {
			return fmt.Errorf("a private key is required for key authentication")
		}
	case "password":
		if !cfg.Has("password") {
			return fmt.Errorf("a password is required for password authentication")
		}
	default:
		return fmt.Errorf("auth must be password or key")
	}
	if fp := strings.TrimSpace(cfg.String("host_key")); fp != "" && !strings.HasPrefix(fp, "SHA256:") {
		return fmt.Errorf("host key fingerprint must look like SHA256:abc...")
	}
	return nil
}

func (d *Driver) Test(ctx context.Context, cfg core.Config, log *slog.Logger) error {
	if err := d.Validate(cfg); err != nil {
		return err
	}
	if cfg.Host() != nil {
		r, err := remoteexec.Open(ctx, cfg, log)
		if err != nil {
			return err
		}
		defer r.Close()
		if err := r.Check(ctx, remoteexec.Command{Script: "true", Label: "shell", Quiet: true}, "remote shell check"); err != nil {
			return err
		}
		log.Info("host reachable", "host", r.HostLabel())
		return nil
	}
	client, err := dial(cfg, log)
	if err != nil {
		return err
	}
	defer client.Close()
	sess, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("open ssh session: %w", err)
	}
	defer sess.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = sess.Close()
		case <-done:
		}
	}()
	if err := sess.Run("true"); err != nil {
		return fmt.Errorf("remote command failed: %w", scrub(cfg, err))
	}
	log.Info("ssh connection ok", "host", cfg.String("host"), "user", cfg.String("user"))
	return nil
}

func (d *Driver) Backup(ctx context.Context, cfg core.Config, log *slog.Logger) (*source.Stream, error) {
	if err := d.Validate(cfg); err != nil {
		return nil, err
	}
	command := strings.TrimSpace(cfg.String("command"))
	if command == "" {
		return nil, fmt.Errorf("command is required")
	}
	if cfg.Host() != nil {
		return backupOnHost(ctx, cfg, command, log)
	}
	st, err := startSession(ctx, cfg, command, nil, log)
	if err != nil {
		return nil, err
	}
	log.Info("streaming remote command", "host", cfg.String("host"), "command", command)
	return &source.Stream{
		Reader:    st,
		Extension: strings.TrimPrefix(cfg.StringOr("extension", "tar"), "."),
		Meta:      map[string]string{"host": cfg.String("host"), "command": command},
	}, nil
}

func (d *Driver) Restore(ctx context.Context, cfg core.Config, r io.Reader, opts source.RestoreOptions, log *slog.Logger) error {
	command := strings.TrimSpace(cfg.String("restore_command"))
	if opts.Params != nil && opts.Params.Has("restore_command") {
		command = strings.TrimSpace(opts.Params.String("restore_command"))
	}
	if command == "" {
		return fmt.Errorf("this ssh source has no restore command configured")
	}
	if cfg.Host() != nil {
		return restoreOnHost(ctx, cfg, command, r, log)
	}
	st, err := startSession(ctx, cfg, command, r, log)
	if err != nil {
		return err
	}
	log.Info("running remote restore command", "host", cfg.String("host"), "command", command)
	lines := bufio.NewScanner(st)
	lines.Buffer(make([]byte, 0, 8192), 64*1024)
	for lines.Scan() {
		if line := strings.TrimSpace(lines.Text()); line != "" {
			log.Info(line, "host", cfg.String("host"))
		}
	}
	return st.Close()
}

type sessionStream struct {
	client *cryptossh.Client
	sess   *cryptossh.Session
	stdout io.Reader
	wg     sync.WaitGroup
	done   chan struct{}
	tailMu sync.Mutex
	tail   []string
	once   sync.Once
	err    error
	eof    bool
	cfg    core.Config
}

func startSession(ctx context.Context, cfg core.Config, command string, stdin io.Reader, log *slog.Logger) (*sessionStream, error) {
	client, err := dial(cfg, log)
	if err != nil {
		return nil, err
	}
	sess, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("open ssh session: %w", err)
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		sess.Close()
		client.Close()
		return nil, fmt.Errorf("remote stdout: %w", err)
	}
	stderr, err := sess.StderrPipe()
	if err != nil {
		sess.Close()
		client.Close()
		return nil, fmt.Errorf("remote stderr: %w", err)
	}
	if stdin != nil {
		sess.Stdin = stdin
	}
	st := &sessionStream{client: client, sess: sess, stdout: stdout, done: make(chan struct{}), cfg: cfg}
	st.wg.Add(1)
	go func() {
		defer st.wg.Done()
		lines := bufio.NewScanner(stderr)
		lines.Buffer(make([]byte, 0, 8192), 64*1024)
		for lines.Scan() {
			line := strings.TrimRight(lines.Text(), "\r")
			if strings.TrimSpace(line) == "" {
				continue
			}
			line = scrub(cfg, errors.New(line)).Error()
			st.addTail(line)
			log.Warn(line, "host", cfg.String("host"))
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
	if err := sess.Start(command); err != nil {
		close(st.done)
		sess.Close()
		client.Close()
		return nil, fmt.Errorf("start remote command: %w", scrub(cfg, err))
	}
	return st, nil
}

func (s *sessionStream) addTail(line string) {
	s.tailMu.Lock()
	defer s.tailMu.Unlock()
	s.tail = append(s.tail, line)
	if len(s.tail) > tailLines {
		s.tail = s.tail[len(s.tail)-tailLines:]
	}
}

func (s *sessionStream) tailText() string {
	s.tailMu.Lock()
	defer s.tailMu.Unlock()
	return strings.Join(s.tail, " | ")
}

func (s *sessionStream) Read(b []byte) (int, error) {
	n, err := s.stdout.Read(b)
	if errors.Is(err, io.EOF) {
		s.eof = true
	}
	return n, err
}

func (s *sessionStream) Close() error {
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
		_ = s.client.Close()
		switch {
		case aborted:
			s.err = fmt.Errorf("remote command was stopped before it finished")
		case waitErr != nil:
			detail := s.tailText()
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
				s.err = fmt.Errorf("remote command failed: %w: %s", scrub(s.cfg, waitErr), detail)
			} else {
				s.err = fmt.Errorf("remote command failed: %w", scrub(s.cfg, waitErr))
			}
		}
	})
	return s.err
}

var (
	_ source.Driver   = (*Driver)(nil)
	_ source.Restorer = (*Driver)(nil)
)
