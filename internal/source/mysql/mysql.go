package mysql

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/source"
	"github.com/arthurr0/backvault/internal/source/internal/procstream"
)

type Driver struct{}

func New() *Driver { return &Driver{} }

func init() { source.Register(New()) }

func (d *Driver) Spec() core.DriverSpec {
	return core.DriverSpec{
		Kind:        "mysql",
		Label:       "MySQL / MariaDB",
		Description: "Logical dump produced by mysqldump or mariadb-dump, restorable with the mysql client.",
		Icon:        "database",
		Category:    "Database",
		Tools:       []string{"mysqldump", "mariadb-dump", "mysql", "mariadb"},
		Capabilities: []string{
			core.CapTest,
			core.CapRestore,
		},
		Fields: []core.Field{
			{
				Name: "host", Label: "Host", Type: core.FieldString, Required: true,
				Default: "127.0.0.1", Placeholder: "127.0.0.1", Group: "Connection",
				Help: "Use 127.0.0.1 rather than localhost to force TCP instead of a Unix socket.",
			},
			{
				Name: "port", Label: "Port", Type: core.FieldPort, Default: 3306,
				Placeholder: "3306", Group: "Connection",
			},
			{
				Name: "socket", Label: "Unix socket", Type: core.FieldPath, Group: "Connection",
				Advanced: true, Placeholder: "/var/run/mysqld/mysqld.sock",
				Help: "When set, the client connects through this socket and ignores host and port.",
			},
			{
				Name: "user", Label: "User", Type: core.FieldString, Required: true,
				Default: "root", Placeholder: "root", Group: "Connection",
			},
			{
				Name: "password", Label: "Password", Type: core.FieldSecret, Secret: true,
				Group: "Connection",
				Help:  "Written to a temporary defaults file with mode 0600 and removed afterwards, so it never appears in the process list.",
			},
			{
				Name: "all_databases", Label: "Dump all databases", Type: core.FieldBool,
				Default: false, Group: "Connection",
				Help: "Dump the whole server with --all-databases. The mysql schema is included.",
			},
			{
				Name: "databases", Label: "Databases", Type: core.FieldStringList, Required: true,
				Group: "Connection", Placeholder: "app_production", ShowIf: map[string]any{"all_databases": false},
				Help: "One database per line. Several databases are dumped with --databases.",
			},
			{
				Name: "single_transaction", Label: "Single transaction", Type: core.FieldBool,
				Default: true, Group: "Options",
				Help: "Consistent dump of InnoDB tables without locking them. Turn off for MyISAM-only servers.",
			},
			{
				Name: "routines", Label: "Include routines", Type: core.FieldBool, Default: true, Group: "Options",
			},
			{
				Name: "triggers", Label: "Include triggers", Type: core.FieldBool, Default: true, Group: "Options",
			},
			{
				Name: "events", Label: "Include events", Type: core.FieldBool, Default: true, Group: "Options",
			},
			{
				Name: "schema_only", Label: "Schema only", Type: core.FieldBool, Default: false,
				Group: "Options", Help: "Dump table definitions without any row data.",
			},
			{
				Name: "extra_args", Label: "Extra arguments", Type: core.FieldStringList,
				Group: "Advanced", Advanced: true, Placeholder: "--hex-blob",
				Help: "Appended verbatim to the dump command, one argument per line.",
			},
			{
				Name: "binary_path", Label: "Binary path", Type: core.FieldPath,
				Group: "Advanced", Advanced: true, Placeholder: "/usr/bin",
				Help: "Directory holding mysqldump and mysql, or the full path to the dump binary. Leave empty to auto-detect mysqldump then mariadb-dump on PATH.",
			},
		},
	}
}

func (d *Driver) Validate(cfg core.Config) error {
	if err := core.ValidateRequired(d.Spec(), cfg); err != nil {
		return err
	}
	if !cfg.Bool("all_databases", false) && len(cfg.StringList("databases")) == 0 {
		return fmt.Errorf("at least one database is required unless all databases are dumped")
	}
	return nil
}

func (d *Driver) Test(ctx context.Context, cfg core.Config, log *slog.Logger) error {
	bin, err := procstream.Lookup(cfg.String("binary_path"), "mysql", "mariadb")
	if err != nil {
		return err
	}
	defaults, cleanup, err := defaultsFile(cfg)
	if err != nil {
		return err
	}
	args := []string{"--defaults-extra-file=" + defaults, "--batch", "--skip-column-names", "-e", "select 1"}
	return procstream.Run(ctx, log, procstream.Command{
		Name: bin, Args: args, Redact: redact(cfg), Label: "mysql", Cleanup: []func(){cleanup},
	})
}

func (d *Driver) Backup(ctx context.Context, cfg core.Config, log *slog.Logger) (*source.Stream, error) {
	if err := d.Validate(cfg); err != nil {
		return nil, err
	}
	bin, err := procstream.Lookup(cfg.String("binary_path"), "mysqldump", "mariadb-dump")
	if err != nil {
		return nil, err
	}
	defaults, cleanup, err := defaultsFile(cfg)
	if err != nil {
		return nil, err
	}
	args := []string{"--defaults-extra-file=" + defaults}
	if cfg.Bool("single_transaction", true) {
		args = append(args, "--single-transaction")
	}
	if cfg.Bool("routines", true) {
		args = append(args, "--routines")
	}
	if cfg.Bool("triggers", true) {
		args = append(args, "--triggers")
	}
	if cfg.Bool("events", true) {
		args = append(args, "--events")
	}
	if cfg.Bool("schema_only", false) {
		args = append(args, "--no-data")
	}
	args = append(args, cfg.StringList("extra_args")...)
	dbs := cfg.StringList("databases")
	switch {
	case cfg.Bool("all_databases", false):
		args = append(args, "--all-databases")
	case len(dbs) == 1:
		args = append(args, dbs[0])
	default:
		args = append(args, "--databases")
		args = append(args, dbs...)
	}
	log.Info("dumping mysql", "host", cfg.String("host"), "databases", scope(cfg), "tool", bin)
	p, err := procstream.Start(ctx, log, procstream.Command{
		Name: bin, Args: args, Redact: redact(cfg), Label: "mysqldump", Cleanup: []func(){cleanup},
	})
	if err != nil {
		return nil, err
	}
	return &source.Stream{
		Reader:    p,
		Extension: "sql",
		Meta: map[string]string{
			"host":      cfg.String("host"),
			"databases": scope(cfg),
		},
	}, nil
}

func (d *Driver) Restore(ctx context.Context, cfg core.Config, r io.Reader, opts source.RestoreOptions, log *slog.Logger) error {
	bin, err := procstream.Lookup(cfg.String("binary_path"), "mysql", "mariadb")
	if err != nil {
		return err
	}
	params := opts.Params
	if params == nil {
		params = core.Config{}
	}
	defaults, cleanup, err := defaultsFile(cfg)
	if err != nil {
		return err
	}
	args := []string{"--defaults-extra-file=" + defaults}
	target := params.String("database")
	if target == "" && !cfg.Bool("all_databases", false) {
		if dbs := cfg.StringList("databases"); len(dbs) == 1 {
			target = dbs[0]
		}
	}
	if target != "" {
		args = append(args, target)
	}
	log.Info("restoring mysql dump", "database", targetLabel(target), "tool", "mysql")
	return procstream.Run(ctx, log, procstream.Command{
		Name: bin, Args: args, Stdin: r, Redact: redact(cfg), Label: "mysql", Cleanup: []func(){cleanup},
	})
}

func defaultsFile(cfg core.Config) (string, func(), error) {
	var b strings.Builder
	b.WriteString("[client]\n")
	if socket := cfg.String("socket"); socket != "" {
		b.WriteString("socket=" + socket + "\n")
	} else {
		b.WriteString("host=" + cfg.StringOr("host", "127.0.0.1") + "\n")
		b.WriteString("port=" + strconv.Itoa(cfg.Int("port", 3306)) + "\n")
		b.WriteString("protocol=TCP\n")
	}
	if u := cfg.String("user"); u != "" {
		b.WriteString("user=" + u + "\n")
	}
	if pw := cfg.String("password"); pw != "" {
		b.WriteString("password=\"" + strings.NewReplacer("\\", "\\\\", "\"", "\\\"").Replace(pw) + "\"\n")
	}
	path, err := procstream.WriteTempFile("", "backvault-mysql-*.cnf", 0o600, []byte(b.String()))
	if err != nil {
		return "", nil, fmt.Errorf("write mysql defaults file: %w", err)
	}
	return path, func() { _ = os.Remove(path) }, nil
}

func redact(cfg core.Config) []string {
	var out []string
	if pw := cfg.String("password"); pw != "" {
		out = append(out, pw)
	}
	return out
}

func scope(cfg core.Config) string {
	if cfg.Bool("all_databases", false) {
		return "all databases"
	}
	return strings.Join(cfg.StringList("databases"), ", ")
}

func targetLabel(target string) string {
	if target == "" {
		return "from dump"
	}
	return target
}

var (
	_ source.Driver   = (*Driver)(nil)
	_ source.Restorer = (*Driver)(nil)
)
