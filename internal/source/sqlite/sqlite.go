package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/source"
	"github.com/arthurr0/backvault/internal/source/internal/procstream"

	_ "modernc.org/sqlite"
)

const magic = "SQLite format 3\x00"

type Driver struct{}

func New() *Driver { return &Driver{} }

func init() { source.Register(New()) }

func (d *Driver) Spec() core.DriverSpec {
	return core.DriverSpec{
		Kind:        "sqlite",
		Label:       "SQLite",
		Description: "Consistent online copy of a SQLite database file, taken while the application keeps using it.",
		Icon:        "database",
		Category:    "Database",
		Tools:       []string{"sqlite3"},
		Capabilities: []string{
			core.CapTest,
			core.CapRestore,
		},
		Fields: []core.Field{
			{
				Name: "path", Label: "Database file", Type: core.FieldPath, Required: true,
				Group: "Connection", Placeholder: "/var/lib/app/app.db",
				Help: "Path to the .db / .sqlite file on the Backvault host. Never copy the file directly while it is in use, this driver takes a proper online backup.",
			},
			{
				Name: "method", Label: "Backup method", Type: core.FieldSelect, Default: "auto",
				Group: "Advanced", Advanced: true,
				Options: []core.FieldOption{
					{Value: "auto", Label: "Automatic (sqlite3 binary when available)"},
					{Value: "sqlite3", Label: "sqlite3 .backup"},
					{Value: "vacuum", Label: "Built-in VACUUM INTO (no external tools)"},
				},
				Help: "VACUUM INTO also compacts the copy. Both methods are consistent.",
			},
			{
				Name: "binary_path", Label: "Binary path", Type: core.FieldPath,
				Group: "Advanced", Advanced: true, Placeholder: "/usr/bin/sqlite3",
				Help: "Full path to sqlite3, or the directory holding it. Leave empty to use PATH.",
			},
		},
	}
}

func (d *Driver) Validate(cfg core.Config) error {
	if err := core.ValidateRequired(d.Spec(), cfg); err != nil {
		return err
	}
	if !filepath.IsAbs(cfg.String("path")) {
		return fmt.Errorf("database file must be an absolute path")
	}
	switch cfg.StringOr("method", "auto") {
	case "auto", "sqlite3", "vacuum":
	default:
		return fmt.Errorf("backup method must be auto, sqlite3 or vacuum")
	}
	return nil
}

func (d *Driver) Test(ctx context.Context, cfg core.Config, log *slog.Logger) error {
	if err := d.Validate(cfg); err != nil {
		return err
	}
	path := cfg.String("path")
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("database file: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory, not a SQLite database", path)
	}
	db, err := sql.Open("sqlite", readOnlyDSN(path))
	if err != nil {
		return fmt.Errorf("open sqlite database: %w", err)
	}
	defer db.Close()
	var tables int
	if err := db.QueryRowContext(ctx, "select count(*) from sqlite_master").Scan(&tables); err != nil {
		return fmt.Errorf("read sqlite schema: %w", err)
	}
	log.Info("sqlite database reachable", "path", path, "objects", tables, "bytes", info.Size())
	return nil
}

func (d *Driver) Backup(ctx context.Context, cfg core.Config, log *slog.Logger) (*source.Stream, error) {
	if err := d.Validate(cfg); err != nil {
		return nil, err
	}
	path := cfg.String("path")
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("database file: %w", err)
	}
	tmp, err := procstream.NewTempDir("sqlite")
	if err != nil {
		return nil, err
	}
	out := tmp.File("backup.sqlite")

	method := cfg.StringOr("method", "auto")
	bin, lookErr := procstream.Lookup(cfg.String("binary_path"), "sqlite3")
	useBinary := method == "sqlite3" || (method == "auto" && lookErr == nil)
	if method == "sqlite3" && lookErr != nil {
		tmp.Remove()
		return nil, lookErr
	}

	if useBinary {
		log.Info("copying sqlite database", "path", path, "method", "sqlite3 .backup")
		err = procstream.Run(ctx, log, procstream.Command{
			Name: bin, Args: []string{path, ".backup '" + strings.ReplaceAll(out, "'", "''") + "'"},
			Label: "sqlite3",
		})
	} else {
		log.Info("copying sqlite database", "path", path, "method", "VACUUM INTO")
		err = vacuumInto(ctx, path, out)
	}
	if err != nil {
		tmp.Remove()
		return nil, err
	}
	reader, size, err := procstream.FileStream(out, tmp.Remove)
	if err != nil {
		tmp.Remove()
		return nil, err
	}
	log.Info("sqlite copy ready", "bytes", size)
	return &source.Stream{
		Reader:    reader,
		Extension: "sqlite",
		Size:      size,
		Meta:      map[string]string{"path": path},
	}, nil
}

func (d *Driver) Restore(ctx context.Context, cfg core.Config, r io.Reader, opts source.RestoreOptions, log *slog.Logger) error {
	target := opts.TargetPath
	if target == "" {
		target = cfg.String("path")
	}
	if target == "" {
		return fmt.Errorf("no target path for the sqlite restore")
	}
	if info, err := os.Stat(target); err == nil {
		if info.IsDir() {
			return fmt.Errorf("target %s is a directory", target)
		}
		if !opts.Overwrite {
			return fmt.Errorf("target %s already exists, enable overwrite to replace it", target)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat target: %w", err)
	}
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create target directory: %w", err)
	}
	head := make([]byte, len(magic))
	n, err := io.ReadFull(r, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read restore stream: %w", err)
	}
	head = head[:n]
	if string(head) != magic {
		return fmt.Errorf("restore stream is not a SQLite database file")
	}
	tmpFile, err := os.CreateTemp(dir, ".backvault-restore-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmpFile.Name()
	defer os.Remove(tmpName)
	if _, err := tmpFile.Write(head); err != nil {
		tmpFile.Close()
		return fmt.Errorf("write restore file: %w", err)
	}
	written, err := io.Copy(tmpFile, readerCtx{ctx: ctx, r: r})
	if err != nil {
		tmpFile.Close()
		return fmt.Errorf("write restore file: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		return fmt.Errorf("sync restore file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close restore file: %w", err)
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return fmt.Errorf("chmod restore file: %w", err)
	}
	for _, sidecar := range []string{target + "-wal", target + "-shm"} {
		if err := os.Remove(sidecar); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", sidecar, err)
		}
	}
	if err := os.Rename(tmpName, target); err != nil {
		return fmt.Errorf("move restored database into place: %w", err)
	}
	log.Info("sqlite database restored", "path", target, "bytes", written+int64(len(head)))
	return nil
}

type readerCtx struct {
	ctx context.Context
	r   io.Reader
}

func (r readerCtx) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(b)
}

func vacuumInto(ctx context.Context, path, out string) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("open sqlite database: %w", err)
	}
	defer db.Close()
	stmt := "VACUUM INTO '" + strings.ReplaceAll(out, "'", "''") + "'"
	if _, err := db.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("vacuum into temporary copy: %w", err)
	}
	return nil
}

func readOnlyDSN(path string) string {
	escaped := strings.NewReplacer("?", "%3f", "#", "%23").Replace(path)
	if !strings.HasPrefix(escaped, "/") {
		escaped = "/" + escaped
	}
	return "file://" + escaped + "?mode=ro"
}

var (
	_ source.Driver   = (*Driver)(nil)
	_ source.Restorer = (*Driver)(nil)
)
