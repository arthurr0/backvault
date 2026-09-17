package redis

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/source"
	"github.com/arthurr0/backvault/internal/source/internal/procstream"
)

type Driver struct{}

func New() *Driver { return &Driver{} }

func init() { source.Register(New()) }

func (d *Driver) Spec() core.DriverSpec {
	return core.DriverSpec{
		Kind:         "redis",
		Label:        "Redis",
		Description:  "Point-in-time RDB snapshot pulled from the server with redis-cli.",
		Icon:         "database",
		Category:     "Database",
		Tools:        []string{"redis-cli"},
		Capabilities: []string{core.CapTest},
		Fields: []core.Field{
			{
				Name: "host", Label: "Host", Type: core.FieldString, Required: true,
				Default: "127.0.0.1", Placeholder: "127.0.0.1", Group: "Connection",
			},
			{
				Name: "port", Label: "Port", Type: core.FieldPort, Default: 6379,
				Placeholder: "6379", Group: "Connection",
			},
			{
				Name: "user", Label: "ACL user", Type: core.FieldString, Group: "Connection",
				Advanced: true, Placeholder: "default",
				Help: "Only needed on Redis 6 or newer with ACLs. Leave empty for password-only authentication.",
			},
			{
				Name: "password", Label: "Password", Type: core.FieldSecret, Secret: true,
				Group: "Connection",
				Help:  "Passed through the REDISCLI_AUTH environment variable, never on the command line.",
			},
			{
				Name: "tls", Label: "Use TLS", Type: core.FieldBool, Default: false, Group: "Connection",
			},
			{
				Name: "tls_skip_verify", Label: "Skip certificate verification", Type: core.FieldBool,
				Default: false, Group: "Connection", Advanced: true, ShowIf: map[string]any{"tls": true},
				Help: "Accept self-signed certificates. Only use this on trusted networks.",
			},
			{
				Name: "extra_args", Label: "Extra arguments", Type: core.FieldStringList,
				Group: "Advanced", Advanced: true, Placeholder: "--cacert=/etc/ssl/redis.pem",
				Help: "Appended verbatim to redis-cli, one argument per line.",
			},
			{
				Name: "binary_path", Label: "Binary path", Type: core.FieldPath,
				Group: "Advanced", Advanced: true, Placeholder: "/usr/bin/redis-cli",
				Help: "Full path to redis-cli, or the directory holding it. Leave empty to use PATH.",
			},
		},
	}
}

func (d *Driver) Validate(cfg core.Config) error {
	return core.ValidateRequired(d.Spec(), cfg)
}

func (d *Driver) Test(ctx context.Context, cfg core.Config, log *slog.Logger) error {
	bin, err := procstream.Lookup(cfg.String("binary_path"), "redis-cli")
	if err != nil {
		return err
	}
	out, err := procstream.Output(ctx, log, procstream.Command{
		Name: bin, Args: append(connArgs(cfg), "PING"), Env: env(cfg),
		Redact: redact(cfg), Label: "redis-cli", Quiet: true,
	})
	if err != nil {
		return err
	}
	if !bytes.Contains(bytes.ToUpper(out), []byte("PONG")) {
		return fmt.Errorf("unexpected reply to PING: %q", bytes.TrimSpace(out))
	}
	return nil
}

func (d *Driver) Backup(ctx context.Context, cfg core.Config, log *slog.Logger) (*source.Stream, error) {
	if err := d.Validate(cfg); err != nil {
		return nil, err
	}
	bin, err := procstream.Lookup(cfg.String("binary_path"), "redis-cli")
	if err != nil {
		return nil, err
	}
	args := append(connArgs(cfg), "--no-auth-warning", "--rdb", "-")
	log.Info("pulling redis rdb snapshot", "host", cfg.String("host"), "port", cfg.Int("port", 6379))
	p, err := procstream.Start(ctx, log, procstream.Command{
		Name: bin, Args: args, Env: env(cfg), Redact: redact(cfg), Label: "redis-cli",
	})
	if err != nil {
		return nil, err
	}
	return &source.Stream{
		Reader:    p,
		Extension: "rdb",
		Meta:      map[string]string{"host": cfg.String("host")},
	}, nil
}

func connArgs(cfg core.Config) []string {
	args := []string{"-h", cfg.StringOr("host", "127.0.0.1"), "-p", strconv.Itoa(cfg.Int("port", 6379))}
	if u := cfg.String("user"); u != "" {
		args = append(args, "--user", u)
	}
	if cfg.Bool("tls", false) {
		args = append(args, "--tls")
		if cfg.Bool("tls_skip_verify", false) {
			args = append(args, "--insecure")
		}
	}
	args = append(args, cfg.StringList("extra_args")...)
	return args
}

func env(cfg core.Config) []string {
	if pw := cfg.String("password"); pw != "" {
		return []string{"REDISCLI_AUTH=" + pw}
	}
	return nil
}

func redact(cfg core.Config) []string {
	if pw := cfg.String("password"); pw != "" {
		return []string{pw}
	}
	return nil
}

var _ source.Driver = (*Driver)(nil)
