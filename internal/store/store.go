package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

const (
	DefaultLimit = 50
	MaxLimit     = 500
)

type Store struct {
	write *sql.DB
	read  *sql.DB
	path  string

	Users        *UserRepo
	Sessions     *SessionRepo
	Tokens       *TokenRepo
	Sources      *SourceRepo
	Hosts        *HostRepo
	Destinations *DestinationRepo
	Jobs         *JobRepo
	Runs         *RunRepo
	Artifacts    *ArtifactRepo
	Channels     *ChannelRepo
	Settings     *SettingsRepo
	Audit        *AuditRepo
	JobStates    *JobStateRepo
	Stats        *StatsRepo
}

func Open(ctx context.Context, path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("create database directory %s: %w", dir, err)
		}
	}

	write, err := openDB(path, true)
	if err != nil {
		return nil, err
	}
	write.SetMaxOpenConns(1)
	write.SetMaxIdleConns(1)
	write.SetConnMaxLifetime(0)

	read, err := openDB(path, false)
	if err != nil {
		_ = write.Close()
		return nil, err
	}
	read.SetMaxOpenConns(8)
	read.SetMaxIdleConns(8)
	read.SetConnMaxLifetime(0)

	if err := write.PingContext(ctx); err != nil {
		_ = write.Close()
		_ = read.Close()
		return nil, fmt.Errorf("open database %s: %w", path, err)
	}

	s := &Store{write: write, read: read, path: path}
	if err := s.migrate(ctx); err != nil {
		_ = s.Close()
		return nil, err
	}
	s.Users = &UserRepo{s: s}
	s.Sessions = &SessionRepo{s: s}
	s.Tokens = &TokenRepo{s: s}
	s.Sources = &SourceRepo{s: s}
	s.Hosts = &HostRepo{s: s}
	s.Destinations = &DestinationRepo{s: s}
	s.Jobs = &JobRepo{s: s}
	s.Runs = &RunRepo{s: s}
	s.Artifacts = &ArtifactRepo{s: s}
	s.Channels = &ChannelRepo{s: s}
	s.Settings = &SettingsRepo{s: s}
	s.Audit = &AuditRepo{s: s}
	s.JobStates = &JobStateRepo{s: s}
	s.Stats = &StatsRepo{s: s}
	return s, nil
}

func openDB(path string, writer bool) (*sql.DB, error) {
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "synchronous(NORMAL)")
	if writer {
		q.Set("_txlock", "immediate")
	}
	dsn := "file:" + path + "?" + q.Encode()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	return db, nil
}

func (s *Store) Close() error {
	var first error
	if s.read != nil {
		if err := s.read.Close(); err != nil {
			first = err
		}
	}
	if s.write != nil {
		if err := s.write.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (s *Store) Path() string { return s.path }

func (s *Store) Ping(ctx context.Context) error {
	if err := s.read.PingContext(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	return nil
}

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.write.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	applied := map[int]bool{}
	rows, err := s.write.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("read schema_migrations: %w", err)
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return fmt.Errorf("scan schema_migrations: %w", err)
		}
		applied[v] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read schema_migrations: %w", err)
	}

	files, err := migrationList()
	if err != nil {
		return err
	}
	for _, m := range files {
		if applied[m.version] {
			continue
		}
		body, err := fs.ReadFile(migrationFS, "migrations/"+m.name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", m.name, err)
		}
		tx, err := s.write.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", m.name, err)
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", m.name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`, m.version, formatTime(time.Now().UTC())); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %s: %w", m.name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", m.name, err)
		}
	}
	return nil
}

type migration struct {
	version int
	name    string
}

func migrationList() ([]migration, error) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("list migrations: %w", err)
	}
	var out []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		parts := strings.SplitN(e.Name(), "_", 2)
		v, err := strconv.Atoi(parts[0])
		if err != nil {
			return nil, fmt.Errorf("migration %s has no numeric prefix: %w", e.Name(), err)
		}
		out = append(out, migration{version: v, name: e.Name()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

func (s *Store) execWrite(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return s.write.ExecContext(ctx, query, args...)
}

func (s *Store) queryRow(ctx context.Context, query string, args ...any) *sql.Row {
	return s.read.QueryRowContext(ctx, query, args...)
}

func (s *Store) query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return s.read.QueryContext(ctx, query, args...)
}

func (s *Store) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}
