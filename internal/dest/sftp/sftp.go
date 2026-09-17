package sftp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/dest"

	sftpclient "github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type Driver struct{}

func New() *Driver { return &Driver{} }

func init() { dest.Register(New()) }

func (d *Driver) Spec() core.DriverSpec {
	return core.DriverSpec{
		Kind:        "sftp",
		Label:       "SFTP",
		Description: "Any SSH server with SFTP enabled, including Hetzner Storage Box, rsync.net and a plain Linux host.",
		Icon:        "server",
		Category:    "Remote",
		Capabilities: []string{
			core.CapTest,
			core.CapBrowse,
		},
		Fields: []core.Field{
			{
				Name: "host", Label: "Host", Type: core.FieldString, Required: true, Group: "Connection",
				Placeholder: "u123456.your-storagebox.de",
			},
			{
				Name: "port", Label: "Port", Type: core.FieldPort, Default: 22, Group: "Connection",
				Help: "22 for a normal SSH server. Hetzner Storage Box uses 23 for the external SFTP endpoint.",
			},
			{
				Name: "user", Label: "User", Type: core.FieldString, Required: true, Group: "Connection",
				Placeholder: "u123456",
			},
			{
				Name: "auth", Label: "Authentication", Type: core.FieldSelect, Default: "password",
				Group: "Connection",
				Options: []core.FieldOption{
					{Value: "password", Label: "Password"},
					{Value: "key", Label: "Private key"},
				},
			},
			{
				Name: "password", Label: "Password", Type: core.FieldSecret, Secret: true, Group: "Connection",
				ShowIf: map[string]any{"auth": "password"},
			},
			{
				Name: "private_key", Label: "Private key", Type: core.FieldText, Secret: true, Group: "Connection",
				ShowIf: map[string]any{"auth": "key"}, Placeholder: "-----BEGIN OPENSSH PRIVATE KEY-----",
				Help: "PEM text of the key. OpenSSH, RSA, ECDSA and Ed25519 keys are supported.",
			},
			{
				Name: "key_passphrase", Label: "Key passphrase", Type: core.FieldSecret, Secret: true,
				Group: "Connection", ShowIf: map[string]any{"auth": "key"},
			},
			{
				Name: "host_key", Label: "Host key fingerprint", Type: core.FieldString, Group: "Connection",
				Advanced: true, Placeholder: "SHA256:xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
				Help: "Leave empty to trust the key on first use. The fingerprint is then written to the run log as a warning so you can pin it here.",
			},
			{
				Name: "base_path", Label: "Base path", Type: core.FieldPath, Group: "Storage",
				Default: ".", Placeholder: "backups/backvault",
				Help: "Directory on the server that holds the artifacts. Relative paths start in the login directory. Missing directories are created.",
			},
			{
				Name: "connect_timeout", Label: "Connect timeout (seconds)", Type: core.FieldInt, Default: 20,
				Group: "Advanced", Advanced: true,
			},
			{
				Name: "concurrent_requests", Label: "Concurrent requests per file", Type: core.FieldInt, Default: 32,
				Group: "Advanced", Advanced: true,
				Help: "Higher values speed up transfers on long-distance links. Lower it if the server complains.",
			},
		},
	}
}

func (d *Driver) Validate(cfg core.Config) error {
	if err := core.ValidateRequired(d.Spec(), cfg); err != nil {
		return err
	}
	switch cfg.StringOr("auth", "password") {
	case "password":
		if !cfg.Has("password") {
			return fmt.Errorf("a password is required for password authentication")
		}
	case "key":
		if !cfg.Has("private_key") {
			return fmt.Errorf("a private key is required for key authentication")
		}
	default:
		return fmt.Errorf("auth must be password or key")
	}
	if fp := strings.TrimSpace(cfg.String("host_key")); fp != "" && !strings.HasPrefix(fp, "SHA256:") {
		return fmt.Errorf("host key fingerprint must look like SHA256:abc...")
	}
	if n := cfg.Int("concurrent_requests", 32); n < 1 || n > 256 {
		return fmt.Errorf("concurrent requests must be between 1 and 256")
	}
	return nil
}

func (d *Driver) Open(ctx context.Context, cfg core.Config, log *slog.Logger) (dest.Client, error) {
	if err := d.Validate(cfg); err != nil {
		return nil, err
	}
	conn, err := dial(cfg, log)
	if err != nil {
		return nil, err
	}
	sc, err := sftpclient.NewClient(conn,
		sftpclient.UseConcurrentWrites(true),
		sftpclient.UseConcurrentReads(true),
		sftpclient.MaxConcurrentRequestsPerFile(cfg.Int("concurrent_requests", 32)),
	)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("start sftp subsystem: %w", scrub(cfg, err))
	}
	base := strings.TrimSuffix(cfg.StringOr("base_path", "."), "/")
	if base == "" {
		base = "."
	}
	return &client{conn: conn, sc: sc, base: base, log: log}, nil
}

type client struct {
	conn *ssh.Client
	sc   *sftpclient.Client
	base string
	log  *slog.Logger
	once sync.Once
}

func (c *client) resolve(p string) (string, error) {
	clean := path.Clean(strings.TrimPrefix(strings.ReplaceAll(p, "\\", "/"), "/"))
	if clean == "." || clean == "/" {
		clean = ""
	}
	if clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return "", fmt.Errorf("path %q escapes the base path", p)
	}
	if c.base == "." || c.base == "" {
		if clean == "" {
			return ".", nil
		}
		return clean, nil
	}
	if clean == "" {
		return c.base, nil
	}
	return c.base + "/" + clean, nil
}

func (c *client) relative(full string) string {
	if c.base == "." || c.base == "" {
		return strings.TrimPrefix(full, "./")
	}
	return strings.TrimPrefix(strings.TrimPrefix(full, c.base), "/")
}

func (c *client) Put(ctx context.Context, p string, r io.Reader, size int64) error {
	full, err := c.resolve(p)
	if err != nil {
		return err
	}
	dir := path.Dir(full)
	if dir != "." && dir != "" {
		if err := c.sc.MkdirAll(dir); err != nil {
			return fmt.Errorf("create remote directory %s: %w", dir, err)
		}
	}
	partial := full + ".partial"
	f, err := c.sc.Create(partial)
	if err != nil {
		return fmt.Errorf("create %s: %w", partial, err)
	}
	written, copyErr := io.Copy(f, &ctxReader{ctx: ctx, r: r})
	closeErr := f.Close()
	if copyErr != nil {
		_ = c.sc.Remove(partial)
		return fmt.Errorf("upload %s: %w", p, copyErr)
	}
	if closeErr != nil {
		_ = c.sc.Remove(partial)
		return fmt.Errorf("finish upload of %s: %w", p, closeErr)
	}
	if size > 0 && written != size {
		_ = c.sc.Remove(partial)
		return fmt.Errorf("short upload of %s: expected %d bytes, wrote %d", p, size, written)
	}
	if err := c.rename(partial, full); err != nil {
		_ = c.sc.Remove(partial)
		return fmt.Errorf("move %s into place: %w", p, err)
	}
	return nil
}

func (c *client) rename(from, to string) error {
	if err := c.sc.PosixRename(from, to); err == nil {
		return nil
	}
	if err := c.sc.Remove(to); err != nil && !isNotExist(err) {
		return err
	}
	return c.sc.Rename(from, to)
}

func (c *client) Get(ctx context.Context, p string) (io.ReadCloser, error) {
	full, err := c.resolve(p)
	if err != nil {
		return nil, err
	}
	f, err := c.sc.Open(full)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", p, err)
	}
	return f, nil
}

func (c *client) Delete(ctx context.Context, p string) error {
	full, err := c.resolve(p)
	if err != nil {
		return err
	}
	if err := c.sc.Remove(full); err != nil {
		if isNotExist(err) {
			return nil
		}
		return fmt.Errorf("delete %s: %w", p, err)
	}
	dir := path.Dir(full)
	for dir != "." && dir != "/" && dir != c.base && strings.HasPrefix(dir, c.base) {
		if err := c.sc.RemoveDirectory(dir); err != nil {
			break
		}
		dir = path.Dir(dir)
	}
	return nil
}

func (c *client) List(ctx context.Context, prefix string) ([]dest.Object, error) {
	start, err := c.resolve(prefix)
	if err != nil {
		return nil, err
	}
	info, err := c.sc.Stat(start)
	if err != nil {
		if isNotExist(err) {
			return []dest.Object{}, nil
		}
		return nil, fmt.Errorf("list %s: %w", prefix, err)
	}
	out := []dest.Object{}
	if !info.IsDir() {
		return append(out, dest.Object{Path: c.relative(start), Size: info.Size(), ModTime: info.ModTime()}), nil
	}
	walker := c.sc.Walk(start)
	for walker.Step() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := walker.Err(); err != nil {
			c.log.Warn("skipping unreadable remote path", "path", walker.Path(), "error", err.Error())
			continue
		}
		if walker.Path() == start {
			continue
		}
		st := walker.Stat()
		if st == nil {
			continue
		}
		name := path.Base(walker.Path())
		if strings.HasSuffix(name, ".partial") {
			continue
		}
		obj := dest.Object{Path: c.relative(walker.Path()), ModTime: st.ModTime(), IsDir: st.IsDir()}
		if !st.IsDir() {
			obj.Size = st.Size()
		}
		out = append(out, obj)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func (c *client) Stat(ctx context.Context, p string) (*dest.Object, error) {
	full, err := c.resolve(p)
	if err != nil {
		return nil, err
	}
	info, err := c.sc.Stat(full)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", p, err)
	}
	return &dest.Object{
		Path:    c.relative(full),
		Size:    info.Size(),
		ModTime: info.ModTime(),
		IsDir:   info.IsDir(),
	}, nil
}

func (c *client) Test(ctx context.Context) error {
	if c.base != "." && c.base != "" {
		if err := c.sc.MkdirAll(c.base); err != nil {
			return fmt.Errorf("create base path %s: %w", c.base, err)
		}
	}
	probe, err := c.resolve(".backvault-write-test")
	if err != nil {
		return err
	}
	f, err := c.sc.Create(probe)
	if err != nil {
		return fmt.Errorf("base path %s is not writable: %w", c.base, err)
	}
	_, writeErr := f.Write([]byte("backvault"))
	closeErr := f.Close()
	_ = c.sc.Remove(probe)
	if writeErr != nil {
		return fmt.Errorf("base path %s is not writable: %w", c.base, writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("base path %s is not writable: %w", c.base, closeErr)
	}
	return nil
}

func (c *client) Close() error {
	var err error
	c.once.Do(func() {
		if c.sc != nil {
			err = c.sc.Close()
		}
		if c.conn != nil {
			_ = c.conn.Close()
		}
	})
	return err
}

func isNotExist(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, fs.ErrNotExist) {
		return true
	}
	var status *sftpclient.StatusError
	if errors.As(err, &status) {
		return status.FxCode() == sftpclient.ErrSSHFxNoSuchFile
	}
	return strings.Contains(strings.ToLower(err.Error()), "file does not exist")
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *ctxReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(b)
}

var (
	_ dest.Driver = (*Driver)(nil)
	_ dest.Client = (*client)(nil)
)
