package command

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
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
		Kind:        "command",
		Label:       "Custom command",
		Description: "Runs a shell command on the Backvault host and uses its standard output as the backup stream.",
		Icon:        "terminal",
		Category:    "Custom",
		Capabilities: []string{
			core.CapTest,
			core.CapRestore,
		},
		Fields: []core.Field{
			{
				Name: "command", Label: "Command", Type: core.FieldText, Required: true, Group: "Command",
				Placeholder: "pg_dump -Fc app",
				Help:        "Runs through the shell. It must write the backup to standard output and exit with code 0. Anything on standard error goes to the run log.",
			},
			{
				Name: "extension", Label: "File extension", Type: core.FieldString, Default: "bin",
				Group: "Command", Placeholder: "bin",
				Help: "Used in the artifact filename, for example tar, sql, dump or bin.",
			},
			{
				Name: "working_dir", Label: "Working directory", Type: core.FieldPath, Group: "Command",
				Placeholder: "/var/lib/app",
			},
			{
				Name: "env", Label: "Environment", Type: core.FieldStringList, Group: "Command",
				Placeholder: "PGPASSWORD=secret",
				Help:        "One KEY=VALUE per line, added to the environment of the command. Values are never written to the run log.",
			},
			{
				Name: "test_command", Label: "Test command", Type: core.FieldText, Group: "Advanced",
				Advanced: true, Placeholder: "pg_isready -q",
				Help: "Optional. Run by the Test connection button. When empty the shell itself is checked.",
			},
			{
				Name: "restore_command", Label: "Restore command", Type: core.FieldText, Group: "Advanced",
				Advanced: true, Placeholder: "psql app",
				Help: "Optional. Receives the unpacked artifact on standard input during a restore.",
			},
			{
				Name: "shell", Label: "Shell", Type: core.FieldString, Default: "/bin/sh", Group: "Advanced",
				Advanced: true, Placeholder: "/bin/sh",
				Help: "Interpreter used to run the commands, called as <shell> -c <command>.",
			},
		},
	}
}

func (d *Driver) Validate(cfg core.Config) error {
	if err := core.ValidateRequired(d.Spec(), cfg); err != nil {
		return err
	}
	for _, e := range cfg.StringList("env") {
		if !strings.Contains(e, "=") {
			return fmt.Errorf("environment entries must look like KEY=VALUE")
		}
	}
	if dir := cfg.String("working_dir"); dir != "" {
		info, err := os.Stat(dir)
		if err != nil {
			return fmt.Errorf("working directory: %w", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("working directory %s is not a directory", dir)
		}
	}
	return nil
}

func (d *Driver) Test(ctx context.Context, cfg core.Config, log *slog.Logger) error {
	if err := d.Validate(cfg); err != nil {
		return err
	}
	shell := cfg.StringOr("shell", "/bin/sh")
	bin, err := procstream.Lookup("", shell)
	if err != nil {
		return fmt.Errorf("shell %s is not available: %w", shell, err)
	}
	test := strings.TrimSpace(cfg.String("test_command"))
	if test == "" {
		log.Info("shell available", "shell", bin)
		return nil
	}
	return procstream.Run(ctx, log, procstream.Command{
		Name: bin, Args: []string{"-c", test}, Env: cfg.StringList("env"),
		Dir: cfg.String("working_dir"), Redact: redact(cfg), Label: "shell",
	})
}

func (d *Driver) Backup(ctx context.Context, cfg core.Config, log *slog.Logger) (*source.Stream, error) {
	if err := d.Validate(cfg); err != nil {
		return nil, err
	}
	shell := cfg.StringOr("shell", "/bin/sh")
	bin, err := procstream.Lookup("", shell)
	if err != nil {
		return nil, fmt.Errorf("shell %s is not available: %w", shell, err)
	}
	log.Info("running backup command", "shell", bin, "workingDir", cfg.String("working_dir"))
	p, err := procstream.Start(ctx, log, procstream.Command{
		Name: bin, Args: []string{"-c", cfg.String("command")}, Env: cfg.StringList("env"),
		Dir: cfg.String("working_dir"), Redact: redact(cfg), Label: "command",
	})
	if err != nil {
		return nil, err
	}
	return &source.Stream{
		Reader:    p,
		Extension: strings.TrimPrefix(cfg.StringOr("extension", "bin"), "."),
	}, nil
}

func (d *Driver) Restore(ctx context.Context, cfg core.Config, r io.Reader, opts source.RestoreOptions, log *slog.Logger) error {
	restore := strings.TrimSpace(cfg.String("restore_command"))
	if opts.Params != nil && opts.Params.Has("restore_command") {
		restore = strings.TrimSpace(opts.Params.String("restore_command"))
	}
	if restore == "" {
		return fmt.Errorf("this command source has no restore command configured")
	}
	shell := cfg.StringOr("shell", "/bin/sh")
	bin, err := procstream.Lookup("", shell)
	if err != nil {
		return fmt.Errorf("shell %s is not available: %w", shell, err)
	}
	log.Info("running restore command", "shell", bin)
	return procstream.Run(ctx, log, procstream.Command{
		Name: bin, Args: []string{"-c", restore}, Env: cfg.StringList("env"),
		Dir: cfg.String("working_dir"), Stdin: r, Redact: redact(cfg), Label: "restore",
	})
}

func redact(cfg core.Config) []string {
	var out []string
	for _, e := range cfg.StringList("env") {
		if i := strings.Index(e, "="); i > 0 {
			if v := e[i+1:]; len(v) >= 3 {
				out = append(out, v)
			}
		}
	}
	return out
}

var (
	_ source.Driver   = (*Driver)(nil)
	_ source.Restorer = (*Driver)(nil)
)
