package local

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/dest"
)

type Driver struct{}

func New() *Driver { return &Driver{} }

func init() { dest.Register(New()) }

func (d *Driver) Spec() core.DriverSpec {
	return core.DriverSpec{
		Kind:        "local",
		Label:       "Local directory",
		Description: "Stores artifacts in a directory on the Backvault host, which can be a mounted NFS share or an external disk.",
		Icon:        "hard-drive",
		Category:    "Filesystem",
		Capabilities: []string{
			core.CapTest,
			core.CapBrowse,
		},
		Fields: []core.Field{
			{
				Name: "path", Label: "Directory", Type: core.FieldPath, Required: true, Group: "Storage",
				Placeholder: "/var/lib/backvault/backups",
				Help:        "Absolute path. It is created if missing. Artifacts are written to a .partial file first and renamed when complete.",
			},
			{
				Name: "dir_mode", Label: "Directory mode", Type: core.FieldString, Default: "0750",
				Group: "Advanced", Advanced: true, Placeholder: "0750",
			},
			{
				Name: "file_mode", Label: "File mode", Type: core.FieldString, Default: "0640",
				Group: "Advanced", Advanced: true, Placeholder: "0640",
			},
			{
				Name: "prune_empty_dirs", Label: "Remove empty directories", Type: core.FieldBool, Default: true,
				Group: "Advanced", Advanced: true,
				Help: "After deleting an artifact, remove directories that became empty, up to the root directory.",
			},
		},
	}
}

func (d *Driver) Validate(cfg core.Config) error {
	if err := core.ValidateRequired(d.Spec(), cfg); err != nil {
		return err
	}
	if !filepath.IsAbs(cfg.String("path")) {
		return fmt.Errorf("directory must be an absolute path")
	}
	if _, err := parseMode(cfg.StringOr("dir_mode", "0750")); err != nil {
		return fmt.Errorf("directory mode: %w", err)
	}
	if _, err := parseMode(cfg.StringOr("file_mode", "0640")); err != nil {
		return fmt.Errorf("file mode: %w", err)
	}
	return nil
}

func (d *Driver) Open(ctx context.Context, cfg core.Config, log *slog.Logger) (dest.Client, error) {
	if err := d.Validate(cfg); err != nil {
		return nil, err
	}
	dirMode, _ := parseMode(cfg.StringOr("dir_mode", "0750"))
	fileMode, _ := parseMode(cfg.StringOr("file_mode", "0640"))
	return &client{
		root:     filepath.Clean(cfg.String("path")),
		dirMode:  dirMode,
		fileMode: fileMode,
		prune:    cfg.Bool("prune_empty_dirs", true),
		log:      log,
	}, nil
}

type client struct {
	root     string
	dirMode  os.FileMode
	fileMode os.FileMode
	prune    bool
	log      *slog.Logger
}

func (c *client) resolve(p string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(p, "/")))
	if clean == "." {
		return c.root, nil
	}
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the destination directory", p)
	}
	full := filepath.Join(c.root, clean)
	rel, err := filepath.Rel(c.root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the destination directory", p)
	}
	return full, nil
}

func (c *client) Put(ctx context.Context, path string, r io.Reader, size int64) error {
	full, err := c.resolve(path)
	if err != nil {
		return err
	}
	dir := filepath.Dir(full)
	if err := os.MkdirAll(dir, c.dirMode); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(full)+".partial-*")
	if err != nil {
		return fmt.Errorf("create temporary file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	written, err := io.Copy(tmp, &ctxReader{ctx: ctx, r: r})
	if err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	if size > 0 && written != size {
		return fmt.Errorf("short write for %s: expected %d bytes, wrote %d", path, size, written)
	}
	if err := os.Chmod(tmpName, c.fileMode); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	if err := os.Rename(tmpName, full); err != nil {
		return fmt.Errorf("move %s into place: %w", path, err)
	}
	if df, err := os.Open(dir); err == nil {
		_ = df.Sync()
		_ = df.Close()
	}
	return nil
}

func (c *client) Get(ctx context.Context, path string) (io.ReadCloser, error) {
	full, err := c.resolve(path)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(full)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return f, nil
}

func (c *client) Delete(ctx context.Context, path string) error {
	full, err := c.resolve(path)
	if err != nil {
		return err
	}
	if err := os.Remove(full); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("delete %s: %w", path, err)
	}
	if c.prune {
		dir := filepath.Dir(full)
		for dir != c.root && strings.HasPrefix(dir, c.root+string(filepath.Separator)) {
			if err := os.Remove(dir); err != nil {
				break
			}
			dir = filepath.Dir(dir)
		}
	}
	return nil
}

func (c *client) List(ctx context.Context, prefix string) ([]dest.Object, error) {
	start, err := c.resolve(prefix)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(start)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []dest.Object{}, nil
		}
		return nil, fmt.Errorf("list %s: %w", prefix, err)
	}
	out := []dest.Object{}
	if !info.IsDir() {
		rel, err := filepath.Rel(c.root, start)
		if err != nil {
			return nil, fmt.Errorf("list %s: %w", prefix, err)
		}
		return append(out, dest.Object{Path: filepath.ToSlash(rel), Size: info.Size(), ModTime: info.ModTime()}), nil
	}
	err = filepath.WalkDir(start, func(p string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, fs.ErrPermission) {
				c.log.Warn("skipping unreadable path", "path", p, "error", walkErr.Error())
				return nil
			}
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if p == start {
			return nil
		}
		rel, err := filepath.Rel(c.root, p)
		if err != nil {
			return err
		}
		name := filepath.Base(p)
		if strings.HasPrefix(name, ".") && strings.Contains(name, ".partial-") {
			return nil
		}
		fi, err := entry.Info()
		if err != nil {
			return nil
		}
		obj := dest.Object{Path: filepath.ToSlash(rel), ModTime: fi.ModTime(), IsDir: entry.IsDir()}
		if !entry.IsDir() {
			obj.Size = fi.Size()
		}
		out = append(out, obj)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", prefix, err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func (c *client) Stat(ctx context.Context, path string) (*dest.Object, error) {
	full, err := c.resolve(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(full)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	rel, err := filepath.Rel(c.root, full)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	return &dest.Object{
		Path:    filepath.ToSlash(rel),
		Size:    info.Size(),
		ModTime: info.ModTime(),
		IsDir:   info.IsDir(),
	}, nil
}

func (c *client) Test(ctx context.Context) error {
	if err := os.MkdirAll(c.root, c.dirMode); err != nil {
		return fmt.Errorf("create directory %s: %w", c.root, err)
	}
	info, err := os.Stat(c.root)
	if err != nil {
		return fmt.Errorf("stat %s: %w", c.root, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", c.root)
	}
	probe, err := os.CreateTemp(c.root, ".backvault-write-test-*")
	if err != nil {
		return fmt.Errorf("directory %s is not writable: %w", c.root, err)
	}
	name := probe.Name()
	_, writeErr := probe.WriteString("backvault")
	closeErr := probe.Close()
	_ = os.Remove(name)
	if writeErr != nil {
		return fmt.Errorf("directory %s is not writable: %w", c.root, writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("directory %s is not writable: %w", c.root, closeErr)
	}
	return nil
}

func (c *client) Close() error { return nil }

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

func parseMode(s string) (os.FileMode, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0o750, nil
	}
	n, err := strconv.ParseUint(s, 8, 32)
	if err != nil {
		return 0, fmt.Errorf("%q is not an octal file mode", s)
	}
	return os.FileMode(n), nil
}

var (
	_ dest.Driver = (*Driver)(nil)
	_ dest.Client = (*client)(nil)
)
