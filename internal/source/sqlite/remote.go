package sqlite

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path"
	"strings"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/remote"
	"github.com/arthurr0/backvault/internal/source"
	"github.com/arthurr0/backvault/internal/source/internal/remoteexec"
)

func copyScript(bin, dbPath, method string) string {
	quoted := remote.ShellQuote(bin) + " " + remote.ShellQuote(dbPath)
	if method == "vacuum" {
		return remoteexec.ScratchScript("rm -f \"$f\"; " + quoted + " \"VACUUM INTO '$f'\" && cat \"$f\"")
	}
	return remoteexec.ScratchScript(quoted + " \".backup '$f'\" && cat \"$f\"")
}

func restoreScript(target string) string {
	q := remote.ShellQuote(target)
	dir := remote.ShellQuote(path.Dir(target))
	return "umask 077; mkdir -p " + dir + " && f=$(mktemp " + remote.ShellQuote(path.Dir(target)+"/.backvault-restore-XXXXXX") +
		") || exit 1; cat > \"$f\" && chmod 644 \"$f\" && rm -f " + remote.ShellQuote(target+"-wal") + " " +
		remote.ShellQuote(target+"-shm") + " && mv \"$f\" " + q + "; rc=$?; rm -f \"$f\"; exit $rc"
}

func (d *Driver) testRemote(ctx context.Context, cfg core.Config, log *slog.Logger) error {
	r, err := remoteexec.Open(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer r.Close()
	dbPath := cfg.String("path")
	if err := r.Check(ctx, remoteexec.Command{
		Script: "test -r " + remote.ShellQuote(dbPath), Label: "test -r", Quiet: true,
	}, "database file "+dbPath+" is not readable on host "+r.HostLabel()); err != nil {
		return err
	}
	bin, err := r.Tool(ctx, cfg.String("binary_path"), "sqlite3")
	if err != nil {
		return err
	}
	res, err := r.Exec(ctx, remoteexec.Command{
		Argv: []string{bin, dbPath, "select count(*) from sqlite_master;"}, Label: "sqlite3", Quiet: true,
	})
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("read sqlite schema: %s", res.Detail())
	}
	log.Info("sqlite database reachable", "path", dbPath, "objects", res.Trimmed(), "host", r.HostLabel())
	return nil
}

func (d *Driver) backupRemote(ctx context.Context, cfg core.Config, log *slog.Logger) (*source.Stream, error) {
	r, err := remoteexec.Open(ctx, cfg, log)
	if err != nil {
		return nil, err
	}
	bin, err := r.Tool(ctx, cfg.String("binary_path"), "sqlite3")
	if err != nil {
		r.Close()
		return nil, fmt.Errorf("sqlite backups on a host need the sqlite3 binary: %w", err)
	}
	method := cfg.StringOr("method", "auto")
	dbPath := cfg.String("path")
	log.Info("copying sqlite database", "path", dbPath, "method", remoteMethod(method), "host", r.HostLabel())
	reader, err := r.Start(ctx, remoteexec.Command{
		Script: copyScript(bin, dbPath, method), Label: "sqlite3",
	})
	if err != nil {
		r.Close()
		return nil, err
	}
	return &source.Stream{
		Reader:    reader,
		Extension: "sqlite",
		Meta:      map[string]string{"path": dbPath, "host": r.HostLabel()},
	}, nil
}

func remoteMethod(method string) string {
	if method == "vacuum" {
		return "VACUUM INTO"
	}
	return "sqlite3 .backup"
}

func (d *Driver) restoreRemote(ctx context.Context, cfg core.Config, src io.Reader, opts source.RestoreOptions, log *slog.Logger) error {
	target := opts.TargetPath
	if target == "" {
		target = cfg.String("path")
	}
	if target == "" {
		return fmt.Errorf("no target path for the sqlite restore")
	}
	target = path.Clean(target)
	r, err := remoteexec.Open(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer r.Close()
	if !opts.Overwrite {
		res, err := r.Exec(ctx, remoteexec.Command{
			Script: "test -e " + remote.ShellQuote(target), Label: "test -e", Quiet: true,
		})
		if err != nil {
			return err
		}
		if res.ExitCode == 0 {
			return fmt.Errorf("target %s already exists on host %s, enable overwrite to replace it", target, r.HostLabel())
		}
	}
	head := make([]byte, len(magic))
	n, err := io.ReadFull(src, head)
	if err != nil && n < len(magic) {
		return fmt.Errorf("read restore stream: %w", err)
	}
	if string(head[:n]) != magic {
		return fmt.Errorf("restore stream is not a SQLite database file")
	}
	log.Info("restoring sqlite database on the host", "path", target, "host", r.HostLabel())
	return r.Run(ctx, remoteexec.Command{
		Script: restoreScript(target),
		Stdin:  io.MultiReader(strings.NewReader(string(head[:n])), src),
		Label:  "restore",
	})
}
