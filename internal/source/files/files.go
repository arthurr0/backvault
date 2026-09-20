package files

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/source"

	"github.com/bmatcuk/doublestar/v4"
)

type Driver struct{}

func New() *Driver { return &Driver{} }

func init() { source.Register(New()) }

func (d *Driver) Spec() core.DriverSpec {
	return core.DriverSpec{
		Kind:        "files",
		Label:       "Files & directories",
		Description: "Streaming tar archive of files and directories on the Backvault host, built in Go without external tools.",
		Icon:        "folder",
		Category:    "Filesystem",
		Capabilities: []string{
			core.CapTest,
			core.CapRestore,
			core.CapRemote,
		},
		Fields: []core.Field{
			{
				Name: "paths", Label: "Paths", Type: core.FieldStringList, Required: true,
				Group: "Source", Placeholder: "/var/www/html",
				Help: "Absolute files or directories, one per line. Directories are archived recursively.",
			},
			{
				Name: "base_dir", Label: "Base directory", Type: core.FieldPath, Group: "Source",
				Placeholder: "/var/www",
				Help:        "Archive entries are stored relative to this directory. Leave empty to store each path under its own name, like tar -C parent.",
			},
			{
				Name: "exclude", Label: "Exclude patterns", Type: core.FieldStringList, Group: "Options",
				Placeholder: "**/node_modules/**",
				Help:        "Glob patterns matched against the path inside the archive. ** matches across directories. A pattern without a slash also matches any file name.",
			},
			{
				Name: "follow_symlinks", Label: "Follow symlinks", Type: core.FieldBool, Default: false,
				Group: "Options",
				Help:  "Archive the contents a symlink points at instead of the link itself. Loops are detected and skipped.",
			},
			{
				Name: "one_file_system", Label: "Stay on one filesystem", Type: core.FieldBool, Default: false,
				Group: "Options",
				Help:  "Skip anything that lives on a different mount than the path it was reached from.",
			},
			{
				Name: "strict", Label: "Fail on unreadable files", Type: core.FieldBool, Default: false,
				Group: "Options",
				Help:  "By default unreadable files are skipped with a warning. Turn this on to fail the whole backup instead.",
			},
			{
				Name: "restore_ownership", Label: "Restore ownership", Type: core.FieldBool, Default: false,
				Group: "Advanced", Advanced: true,
				Help: "Apply the stored user and group on restore. Needs root, failures are logged and ignored.",
			},
		},
	}
}

func (d *Driver) Validate(cfg core.Config) error {
	if err := core.ValidateRequired(d.Spec(), cfg); err != nil {
		return err
	}
	paths := cfg.StringList("paths")
	if len(paths) == 0 {
		return fmt.Errorf("at least one path is required")
	}
	for _, p := range paths {
		if !filepath.IsAbs(p) {
			return fmt.Errorf("path %q must be absolute", p)
		}
	}
	if base := cfg.String("base_dir"); base != "" && !filepath.IsAbs(base) {
		return fmt.Errorf("base directory must be absolute")
	}
	for _, pattern := range cfg.StringList("exclude") {
		if !doublestar.ValidatePattern(pattern) {
			return fmt.Errorf("exclude pattern %q is not valid", pattern)
		}
	}
	return nil
}

func (d *Driver) Test(ctx context.Context, cfg core.Config, log *slog.Logger) error {
	if err := d.Validate(cfg); err != nil {
		return err
	}
	if cfg.Host() != nil {
		return d.testRemote(ctx, cfg, log)
	}
	var problems []string
	var total int64
	for _, p := range cfg.StringList("paths") {
		info, err := os.Lstat(p)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", p, err))
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 && !cfg.Bool("follow_symlinks", false) {
			continue
		}
		if info.IsDir() {
			f, err := os.Open(p)
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s: %v", p, err))
				continue
			}
			_, err = f.Readdirnames(1)
			f.Close()
			if err != nil && !errors.Is(err, io.EOF) {
				problems = append(problems, fmt.Sprintf("%s: %v", p, err))
			}
			continue
		}
		f, err := os.Open(p)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", p, err))
			continue
		}
		f.Close()
		total += info.Size()
	}
	if len(problems) > 0 {
		return fmt.Errorf("unreadable paths: %s", strings.Join(problems, "; "))
	}
	if base := cfg.String("base_dir"); base != "" {
		if _, err := os.Stat(base); err != nil {
			return fmt.Errorf("base directory: %w", err)
		}
	}
	log.Info("all paths readable", "paths", len(cfg.StringList("paths")))
	return nil
}

func (d *Driver) Backup(ctx context.Context, cfg core.Config, log *slog.Logger) (*source.Stream, error) {
	if err := d.Validate(cfg); err != nil {
		return nil, err
	}
	if cfg.Host() != nil {
		return d.backupRemote(ctx, cfg, log)
	}
	w := &walker{
		log:        log,
		exclude:    cfg.StringList("exclude"),
		follow:     cfg.Bool("follow_symlinks", false),
		oneFS:      cfg.Bool("one_file_system", false),
		strict:     cfg.Bool("strict", false),
		hardlinks:  map[string]string{},
		visitedDir: map[string]bool{},
	}
	if base := strings.TrimSpace(cfg.String("base_dir")); base != "" {
		w.baseDir = filepath.Clean(base)
	}
	paths := make([]string, 0, len(cfg.StringList("paths")))
	for _, p := range cfg.StringList("paths") {
		paths = append(paths, filepath.Clean(p))
	}

	pr, pw := io.Pipe()
	runCtx, cancel := context.WithCancel(ctx)
	st := &tarStream{pr: pr, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(st.done)
		tw := tar.NewWriter(pw)
		err := w.run(runCtx, tw, paths)
		if err == nil {
			err = tw.Close()
		}
		st.err = err
		if err != nil {
			pw.CloseWithError(err)
			return
		}
		pw.Close()
		log.Info("archive complete", "files", w.files, "bytes", w.bytes, "skipped", w.skipped)
	}()
	return &source.Stream{
		Reader:    st,
		Extension: "tar",
		Meta:      map[string]string{"paths": strings.Join(paths, ", ")},
	}, nil
}

type tarStream struct {
	pr     *io.PipeReader
	cancel context.CancelFunc
	done   chan struct{}
	err    error
	once   sync.Once
	eof    bool
}

func (s *tarStream) Read(b []byte) (int, error) {
	n, err := s.pr.Read(b)
	if errors.Is(err, io.EOF) {
		s.eof = true
	}
	return n, err
}

func (s *tarStream) Close() error {
	s.once.Do(func() {
		if !s.eof {
			s.cancel()
			s.pr.CloseWithError(errors.New("archive reader closed before the archive was complete"))
		}
		<-s.done
		s.cancel()
		_ = s.pr.Close()
	})
	return s.err
}

type walker struct {
	log        *slog.Logger
	exclude    []string
	follow     bool
	oneFS      bool
	strict     bool
	baseDir    string
	hardlinks  map[string]string
	visitedDir map[string]bool
	seen       map[string]bool
	files      int64
	bytes      int64
	skipped    int64
}

func (w *walker) run(ctx context.Context, tw *tar.Writer, paths []string) error {
	w.seen = map[string]bool{}
	for _, root := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		base := w.baseDir
		if base == "" || !within(base, root) {
			base = filepath.Dir(root)
		}
		info, err := os.Lstat(root)
		if err != nil {
			if w.strict {
				return fmt.Errorf("stat %s: %w", root, err)
			}
			w.log.Warn("skipping unreadable path", "path", root, "error", err.Error())
			w.skipped++
			continue
		}
		var rootDev uint64
		var haveDev bool
		if w.oneFS {
			rootDev, haveDev = deviceOf(info)
		}
		if err := w.walk(ctx, tw, base, root, info, rootDev, haveDev); err != nil {
			return err
		}
	}
	return nil
}

func (w *walker) walk(ctx context.Context, tw *tar.Writer, base, full string, info os.FileInfo, rootDev uint64, haveDev bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	name, ok := w.entryName(base, full)
	if !ok {
		return nil
	}
	if name != "" && w.excluded(name, info.IsDir()) {
		w.skipped++
		return nil
	}
	if w.oneFS && haveDev && name != "" {
		if dev, ok := deviceOf(info); ok && dev != rootDev {
			w.log.Warn("skipping other filesystem", "path", full)
			w.skipped++
			return nil
		}
	}
	if info.Mode()&os.ModeSymlink != 0 && w.follow {
		target, err := os.Stat(full)
		if err != nil {
			if w.strict {
				return fmt.Errorf("resolve symlink %s: %w", full, err)
			}
			w.log.Warn("skipping broken symlink", "path", full, "error", err.Error())
			w.skipped++
			return nil
		}
		resolved, err := filepath.EvalSymlinks(full)
		if err == nil && target.IsDir() {
			if w.visitedDir[resolved] {
				w.log.Warn("skipping symlink loop", "path", full, "target", resolved)
				w.skipped++
				return nil
			}
			w.visitedDir[resolved] = true
		}
		info = target
	}

	switch {
	case info.IsDir():
		if err := w.writeHeader(tw, name+"/", full, info, ""); err != nil {
			return err
		}
		entries, err := os.ReadDir(full)
		if err != nil {
			if w.strict {
				return fmt.Errorf("read directory %s: %w", full, err)
			}
			w.log.Warn("skipping unreadable directory", "path", full, "error", err.Error())
			w.skipped++
			return nil
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, e := range entries {
			childInfo, err := e.Info()
			if err != nil {
				if w.strict {
					return fmt.Errorf("stat %s: %w", filepath.Join(full, e.Name()), err)
				}
				w.log.Warn("skipping unreadable entry", "path", filepath.Join(full, e.Name()), "error", err.Error())
				w.skipped++
				continue
			}
			if err := w.walk(ctx, tw, base, filepath.Join(full, e.Name()), childInfo, rootDev, haveDev); err != nil {
				return err
			}
		}
		return nil
	case info.Mode()&os.ModeSymlink != 0:
		link, err := os.Readlink(full)
		if err != nil {
			if w.strict {
				return fmt.Errorf("read symlink %s: %w", full, err)
			}
			w.log.Warn("skipping unreadable symlink", "path", full, "error", err.Error())
			w.skipped++
			return nil
		}
		return w.writeHeader(tw, name, full, info, link)
	case info.Mode()&os.ModeSocket != 0:
		w.log.Warn("skipping socket", "path", full)
		w.skipped++
		return nil
	case info.Mode().IsRegular():
		if key, ok := hardlinkKey(info); ok {
			if first, seen := w.hardlinks[key]; seen {
				hdr, err := tar.FileInfoHeader(info, "")
				if err != nil {
					return fmt.Errorf("header for %s: %w", full, err)
				}
				hdr.Name = name
				hdr.Typeflag = tar.TypeLink
				hdr.Linkname = first
				hdr.Size = 0
				fillOwner(hdr, info)
				if err := tw.WriteHeader(hdr); err != nil {
					return fmt.Errorf("write header for %s: %w", full, err)
				}
				w.files++
				return nil
			}
			w.hardlinks[key] = name
		}
		return w.writeFile(ctx, tw, name, full, info)
	default:
		return w.writeHeader(tw, name, full, info, "")
	}
}

func (w *walker) writeFile(ctx context.Context, tw *tar.Writer, name, full string, info os.FileInfo) error {
	f, err := os.Open(full)
	if err != nil {
		if w.strict {
			return fmt.Errorf("open %s: %w", full, err)
		}
		w.log.Warn("skipping unreadable file", "path", full, "error", err.Error())
		w.skipped++
		return nil
	}
	defer f.Close()
	hdr, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return fmt.Errorf("header for %s: %w", full, err)
	}
	hdr.Name = name
	hdr.Size = info.Size()
	fillOwner(hdr, info)
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("write header for %s: %w", full, err)
	}
	written, err := io.CopyN(tw, &ctxReader{ctx: ctx, r: f}, info.Size())
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read %s: %w", full, err)
	}
	if written < info.Size() {
		w.log.Warn("file shrank while being archived, padding with zeros", "path", full, "expected", info.Size(), "read", written)
		if _, err := io.CopyN(tw, zeroReader{}, info.Size()-written); err != nil {
			return fmt.Errorf("pad %s: %w", full, err)
		}
	}
	w.files++
	w.bytes += info.Size()
	return nil
}

func (w *walker) writeHeader(tw *tar.Writer, name, full string, info os.FileInfo, link string) error {
	if name == "" || name == "/" {
		return nil
	}
	hdr, err := tar.FileInfoHeader(info, link)
	if err != nil {
		return fmt.Errorf("header for %s: %w", full, err)
	}
	hdr.Name = name
	fillOwner(hdr, info)
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("write header for %s: %w", full, err)
	}
	w.files++
	return nil
}

func (w *walker) entryName(base, full string) (string, bool) {
	rel, err := filepath.Rel(base, full)
	if err != nil {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || rel == "" {
		return "", false
	}
	if strings.HasPrefix(rel, "../") {
		rel = path.Base(full)
	}
	if w.seen[rel] {
		return "", false
	}
	w.seen[rel] = true
	return rel, true
}

func (w *walker) excluded(name string, isDir bool) bool {
	candidates := []string{name}
	if isDir {
		candidates = append(candidates, name+"/")
	}
	for _, pattern := range w.exclude {
		for _, c := range candidates {
			if ok, _ := doublestar.Match(pattern, strings.TrimSuffix(c, "/")); ok {
				return true
			}
		}
		if !strings.Contains(pattern, "/") {
			if ok, _ := doublestar.Match(pattern, path.Base(name)); ok {
				return true
			}
		}
	}
	return false
}

func within(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
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

type zeroReader struct{}

func (zeroReader) Read(b []byte) (int, error) {
	for i := range b {
		b[i] = 0
	}
	return len(b), nil
}

var (
	_ source.Driver   = (*Driver)(nil)
	_ source.Restorer = (*Driver)(nil)
)
