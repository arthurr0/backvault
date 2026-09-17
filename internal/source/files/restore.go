package files

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/source"
)

func (d *Driver) Restore(ctx context.Context, cfg core.Config, r io.Reader, opts source.RestoreOptions, log *slog.Logger) error {
	root := opts.TargetPath
	if root == "" {
		root = cfg.String("base_dir")
	}
	if root == "" {
		return fmt.Errorf("a target directory is required to restore a file archive")
	}
	if !filepath.IsAbs(root) {
		return fmt.Errorf("target directory must be an absolute path")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("create target directory: %w", err)
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("resolve target directory: %w", err)
	}
	chown := cfg.Bool("restore_ownership", false)

	type deferred struct {
		path    string
		mode    os.FileMode
		modTime time.Time
	}
	var dirs []deferred

	tr := tar.NewReader(&ctxReader{ctx: ctx, r: r})
	var files, skipped int64
	var bytes int64
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read archive: %w", err)
		}
		target, err := safeJoin(realRoot, hdr.Name)
		if err != nil {
			return fmt.Errorf("refusing entry %q: %w", hdr.Name, err)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("create directory %s: %w", target, err)
			}
			dirs = append(dirs, deferred{path: target, mode: hdr.FileInfo().Mode(), modTime: hdr.ModTime})
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create directory for %s: %w", target, err)
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, hdr.FileInfo().Mode().Perm())
			if err != nil {
				return fmt.Errorf("create %s: %w", target, err)
			}
			n, err := io.Copy(f, tr)
			if err != nil {
				f.Close()
				return fmt.Errorf("write %s: %w", target, err)
			}
			if err := f.Close(); err != nil {
				return fmt.Errorf("close %s: %w", target, err)
			}
			if err := os.Chmod(target, hdr.FileInfo().Mode().Perm()); err != nil {
				return fmt.Errorf("chmod %s: %w", target, err)
			}
			_ = os.Chtimes(target, hdr.ModTime, hdr.ModTime)
			files++
			bytes += n
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create directory for %s: %w", target, err)
			}
			if err := checkLinkTarget(realRoot, target, hdr.Linkname); err != nil {
				return fmt.Errorf("refusing symlink %q: %w", hdr.Name, err)
			}
			_ = os.Remove(target)
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return fmt.Errorf("create symlink %s: %w", target, err)
			}
			files++
		case tar.TypeLink:
			linkSource, err := safeJoin(realRoot, hdr.Linkname)
			if err != nil {
				return fmt.Errorf("refusing hard link %q: %w", hdr.Name, err)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create directory for %s: %w", target, err)
			}
			_ = os.Remove(target)
			if err := os.Link(linkSource, target); err != nil {
				return fmt.Errorf("create hard link %s: %w", target, err)
			}
			files++
		default:
			log.Warn("skipping unsupported archive entry", "name", hdr.Name, "type", string(rune(hdr.Typeflag)))
			skipped++
			continue
		}
		if chown && hdr.Uid >= 0 && hdr.Gid >= 0 {
			if err := os.Lchown(target, hdr.Uid, hdr.Gid); err != nil {
				log.Warn("could not restore ownership", "path", target, "error", err.Error())
			}
		}
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := os.Chmod(dirs[i].path, dirs[i].mode.Perm()); err != nil {
			log.Warn("could not restore directory mode", "path", dirs[i].path, "error", err.Error())
		}
		_ = os.Chtimes(dirs[i].path, dirs[i].modTime, dirs[i].modTime)
	}
	log.Info("archive extracted", "target", root, "entries", files, "bytes", bytes, "skipped", skipped)
	return nil
}

func safeJoin(root, name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, string(filepath.Separator)) {
		return "", errors.New("absolute paths are not allowed")
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes the target directory")
	}
	joined := filepath.Join(root, clean)
	if !within(root, joined) {
		return "", errors.New("path escapes the target directory")
	}
	return joined, nil
}

func checkLinkTarget(root, linkPath, linkname string) error {
	resolved := linkname
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(filepath.Dir(linkPath), resolved)
	}
	if !within(root, filepath.Clean(resolved)) {
		return errors.New("link target escapes the target directory")
	}
	return nil
}
