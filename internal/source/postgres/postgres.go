package postgres

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/source"
	"github.com/arthurr0/backvault/internal/source/internal/remoteexec"
)

type Driver struct{}

func New() *Driver { return &Driver{} }

func init() { source.Register(New()) }

func (d *Driver) Spec() core.DriverSpec {
	return core.DriverSpec{
		Kind:        "postgres",
		Label:       "PostgreSQL",
		Description: "Logical dump of a PostgreSQL database with pg_dump, or of the whole cluster with pg_dumpall.",
		Icon:        "database",
		Category:    "Database",
		Tools:       []string{"pg_dump", "pg_dumpall", "pg_restore", "psql"},
		Capabilities: []string{
			core.CapTest,
			core.CapRestore,
			core.CapRemote,
		},
		Fields: []core.Field{
			{
				Name: "host", Label: "Host", Type: core.FieldString, Required: true,
				Default: "localhost", Placeholder: "localhost", Group: "Connection",
				Help: "Hostname, IP address or path to the Unix socket directory.",
			},
			{
				Name: "port", Label: "Port", Type: core.FieldPort, Default: 5432,
				Placeholder: "5432", Group: "Connection",
			},
			{
				Name: "user", Label: "User", Type: core.FieldString, Required: true,
				Default: "postgres", Placeholder: "postgres", Group: "Connection",
				Help: "Role used for the dump. It needs read access to everything you want backed up.",
			},
			{
				Name: "password", Label: "Password", Type: core.FieldSecret, Secret: true,
				Group: "Connection",
				Help:  "Passed to the tools through PGPASSWORD, never on the command line. Leave empty for peer, trust or .pgpass authentication.",
			},
			{
				Name: "database", Label: "Database", Type: core.FieldString, Required: true,
				Placeholder: "app_production", Group: "Connection",
				ShowIf: map[string]any{"all_databases": false},
			},
			{
				Name: "all_databases", Label: "Dump all databases", Type: core.FieldBool,
				Default: false, Group: "Connection",
				Help: "Use pg_dumpall to dump every database plus roles and tablespaces. The output is always plain SQL.",
			},
			{
				Name: "sslmode", Label: "SSL mode", Type: core.FieldSelect, Default: "prefer",
				Group: "Connection",
				Options: []core.FieldOption{
					{Value: "disable", Label: "disable"},
					{Value: "allow", Label: "allow"},
					{Value: "prefer", Label: "prefer"},
					{Value: "require", Label: "require"},
					{Value: "verify-ca", Label: "verify-ca"},
					{Value: "verify-full", Label: "verify-full"},
				},
			},
			{
				Name: "format", Label: "Format", Type: core.FieldSelect, Default: "custom",
				Group: "Options", ShowIf: map[string]any{"all_databases": false},
				Options: []core.FieldOption{
					{Value: "custom", Label: "Custom (.dump, compressed, restores with pg_restore)"},
					{Value: "plain", Label: "Plain SQL (.sql, restores with psql)"},
				},
				Help: "Custom format is smaller and allows selective restores. Plain SQL is readable and portable.",
			},
			{
				Name: "schema_only", Label: "Schema only", Type: core.FieldBool, Default: false,
				Group: "Options", Help: "Dump table definitions without any row data.",
			},
			{
				Name: "exclude_tables", Label: "Exclude tables", Type: core.FieldStringList,
				Group: "Options", Placeholder: "public.sessions", ShowIf: map[string]any{"all_databases": false},
				Help: "Table patterns passed to --exclude-table. Wildcards are allowed, one entry per line.",
			},
			{
				Name: "extra_args", Label: "Extra arguments", Type: core.FieldStringList,
				Group: "Advanced", Advanced: true, Placeholder: "--no-owner",
				Help: "Appended verbatim to the dump command, one argument per line.",
			},
			{
				Name: "binary_path", Label: "Binary path", Type: core.FieldPath,
				Group: "Advanced", Advanced: true, Placeholder: "/usr/lib/postgresql/16/bin",
				Help: "Directory holding pg_dump, pg_dumpall, pg_restore and psql, or the full path to pg_dump. Leave empty to use PATH.",
			},
		},
	}
}

func (d *Driver) Validate(cfg core.Config) error {
	if err := core.ValidateRequired(d.Spec(), cfg); err != nil {
		return err
	}
	if !cfg.Bool("all_databases", false) && !cfg.Has("database") {
		return fmt.Errorf("database is required unless all databases are dumped")
	}
	switch cfg.StringOr("format", "custom") {
	case "custom", "plain":
	default:
		return fmt.Errorf("format must be custom or plain")
	}
	return nil
}

func (d *Driver) Test(ctx context.Context, cfg core.Config, log *slog.Logger) error {
	r, err := remoteexec.Open(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer r.Close()
	db := cfg.StringOr("database", "postgres")
	if bin, err := r.Tool(ctx, cfg.String("binary_path"), "psql"); err == nil {
		argv := append([]string{bin}, append(connArgs(cfg), "-d", db, "-w", "-tAX", "-c", "select 1")...)
		return r.Run(ctx, remoteexec.Command{Argv: argv, Env: env(cfg), Redact: redact(cfg), Label: "psql"})
	}
	bin, err := r.Tool(ctx, cfg.String("binary_path"), "pg_isready")
	if err != nil {
		return fmt.Errorf("neither psql nor pg_isready is available: %w", err)
	}
	argv := append([]string{bin}, append(connArgs(cfg), "-d", db)...)
	return r.Run(ctx, remoteexec.Command{Argv: argv, Env: env(cfg), Redact: redact(cfg), Label: "pg_isready"})
}

func dumpArgs(cfg core.Config) (args []string, ext string) {
	ext = "sql"
	if cfg.Bool("all_databases", false) {
		args = append(connArgs(cfg), "-w")
		if cfg.Bool("schema_only", false) {
			args = append(args, "--schema-only")
		}
	} else {
		args = append(connArgs(cfg), "-w", "-d", cfg.String("database"))
		if cfg.StringOr("format", "custom") == "custom" {
			args = append(args, "-F", "c")
			ext = "dump"
		} else {
			args = append(args, "-F", "p")
		}
		if cfg.Bool("schema_only", false) {
			args = append(args, "--schema-only")
		}
		for _, t := range cfg.StringList("exclude_tables") {
			args = append(args, "--exclude-table="+t)
		}
	}
	return append(args, cfg.StringList("extra_args")...), ext
}

func (d *Driver) Backup(ctx context.Context, cfg core.Config, log *slog.Logger) (*source.Stream, error) {
	if err := d.Validate(cfg); err != nil {
		return nil, err
	}
	r, err := remoteexec.Open(ctx, cfg, log)
	if err != nil {
		return nil, err
	}
	all := cfg.Bool("all_databases", false)
	tool := "pg_dump"
	if all {
		tool = "pg_dumpall"
	}
	bin, err := r.Tool(ctx, cfg.String("binary_path"), tool)
	if err != nil {
		r.Close()
		return nil, err
	}
	args, ext := dumpArgs(cfg)
	log.Info("dumping postgres", "host", cfg.String("host"), "database", scope(cfg), "format", ext, "where", where(r))
	reader, err := r.Start(ctx, remoteexec.Command{
		Argv: append([]string{bin}, args...), Env: env(cfg), Redact: redact(cfg), Label: tool,
	})
	if err != nil {
		r.Close()
		return nil, err
	}
	return &source.Stream{
		Reader:    reader,
		Extension: ext,
		Meta: map[string]string{
			"host":     cfg.String("host"),
			"database": scope(cfg),
			"format":   ext,
		},
	}, nil
}

func (d *Driver) Restore(ctx context.Context, cfg core.Config, src io.Reader, opts source.RestoreOptions, log *slog.Logger) error {
	params := opts.Params
	if params == nil {
		params = core.Config{}
	}
	r, err := remoteexec.Open(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer r.Close()
	target := params.StringOr("database", cfg.StringOr("database", "postgres"))
	if strings.EqualFold(opts.Extension, "dump") {
		bin, err := r.Tool(ctx, cfg.String("binary_path"), "pg_restore")
		if err != nil {
			return err
		}
		args := append(connArgs(cfg), "-w", "-d", target)
		if params.Bool("clean", false) || params.Bool("drop", false) {
			args = append(args, "--clean")
		}
		if params.Bool("if_exists", params.Bool("clean", false)) {
			args = append(args, "--if-exists")
		}
		if params.Bool("create", false) {
			args = append(args, "--create")
		}
		if params.Bool("no_owner", true) {
			args = append(args, "--no-owner")
		}
		log.Info("restoring postgres dump", "database", target, "tool", "pg_restore", "where", where(r))
		return r.Run(ctx, remoteexec.Command{
			Argv: append([]string{bin}, args...), Env: env(cfg), Stdin: src,
			Redact: redact(cfg), Label: "pg_restore",
		})
	}
	bin, err := r.Tool(ctx, cfg.String("binary_path"), "psql")
	if err != nil {
		return err
	}
	args := append(connArgs(cfg), "-w", "-d", target, "-X", "-f", "-")
	if params.Bool("stop_on_error", true) {
		args = append(args, "-v", "ON_ERROR_STOP=1")
	}
	if params.Bool("single_transaction", false) {
		args = append(args, "--single-transaction")
	}
	log.Info("restoring postgres sql", "database", target, "tool", "psql", "where", where(r))
	return r.Run(ctx, remoteexec.Command{
		Argv: append([]string{bin}, args...), Env: env(cfg), Stdin: src,
		Redact: redact(cfg), Label: "psql",
	})
}

func where(r *remoteexec.Runner) string {
	if r.Remote() {
		return "host " + r.HostLabel()
	}
	return "this server"
}

func connArgs(cfg core.Config) []string {
	args := []string{"-h", cfg.StringOr("host", "localhost")}
	if port := cfg.Int("port", 5432); port > 0 {
		args = append(args, "-p", strconv.Itoa(port))
	}
	if u := cfg.String("user"); u != "" {
		args = append(args, "-U", u)
	}
	return args
}

func env(cfg core.Config) []string {
	out := []string{}
	if pw := cfg.String("password"); pw != "" {
		out = append(out, "PGPASSWORD="+pw)
	}
	if mode := cfg.String("sslmode"); mode != "" {
		out = append(out, "PGSSLMODE="+mode)
	}
	out = append(out, "PGCONNECT_TIMEOUT=15", "PGCLIENTENCODING=UTF8")
	return out
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
	return cfg.String("database")
}

var (
	_ source.Driver   = (*Driver)(nil)
	_ source.Restorer = (*Driver)(nil)
)
