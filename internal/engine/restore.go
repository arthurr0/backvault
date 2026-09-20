package engine

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

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/source"
)

type RestoreMode string

const (
	RestoreToPath   RestoreMode = "path"
	RestoreToSource RestoreMode = "source"
)

type RestoreRequest struct {
	Artifact       core.Artifact
	Mode           RestoreMode
	TargetPath     string
	Extract        bool
	TargetSourceID string
	Passphrase     string
	Params         core.Config
	CreatedBy      string
}

func (e *Engine) EnqueueRestore(ctx context.Context, req RestoreRequest) (core.Run, error) {
	switch req.Mode {
	case RestoreToPath:
		if strings.TrimSpace(req.TargetPath) == "" {
			return core.Run{}, errors.New("targetPath is required for a path restore")
		}
	case RestoreToSource:
	default:
		return core.Run{}, fmt.Errorf("unsupported restore mode %q", req.Mode)
	}

	job := core.Job{ID: req.Artifact.JobID, Slug: req.Artifact.JobSlug, Name: req.Artifact.JobName}
	run := e.newRun(job, core.RunRestore, core.TriggerManual, req.CreatedBy)
	run.Meta["artifactId"] = req.Artifact.ID
	run.Meta["mode"] = string(req.Mode)
	if req.Mode == RestoreToPath {
		run.Meta["targetPath"] = req.TargetPath
	}
	saved, err := e.store.Runs.Create(ctx, run)
	if err != nil {
		return core.Run{}, err
	}
	request := req
	t := &task{run: saved, execute: func(c context.Context, rs *runState) error {
		return e.runRestore(c, rs, request)
	}}
	if err := e.enqueue(ctx, t); err != nil {
		return core.Run{}, err
	}
	return saved, nil
}

func (e *Engine) runRestore(ctx context.Context, rs *runState, req RestoreRequest) error {
	log := rs.Logger()
	prepare := rs.stage(ctx, "prepare")
	artifact, err := e.store.Artifacts.Get(ctx, req.Artifact.ID)
	if err != nil {
		return prepare.Fail(ctx, err)
	}
	if artifact.Status != core.ArtifactPresent {
		return prepare.Fail(ctx, fmt.Errorf("artifact status is %s", artifact.Status))
	}
	prepare.Done(ctx, artifact.Filename)

	download := rs.stage(ctx, "download")
	reader, name, err := e.OpenArtifact(ctx, artifact, DownloadOptions{Passphrase: req.Passphrase}, log)
	if err != nil {
		return download.Fail(ctx, err)
	}
	defer reader.Close()
	download.Done(ctx, fmt.Sprintf("streaming %s", artifact.Filename))

	restore := rs.stage(ctx, "restore")
	counter := &countingReader{r: reader, ctx: ctx}
	switch req.Mode {
	case RestoreToPath:
		if err := restoreToPath(ctx, counter, name, req, artifact, log); err != nil {
			return restore.Fail(ctx, err)
		}
	case RestoreToSource:
		if err := e.restoreToSource(ctx, counter, req, artifact, log); err != nil {
			return restore.Fail(ctx, err)
		}
	}
	rs.setResult(ctx, artifact.Filename, artifact.SHA256, counter.n, artifact.Size)
	restore.Done(ctx, fmt.Sprintf("%d bytes restored", counter.n))
	return nil
}

func restoreToPath(ctx context.Context, r io.Reader, name string, req RestoreRequest, artifact core.Artifact, log *slog.Logger) error {
	target := req.TargetPath
	isTar := req.Extract && (artifact.Extension == "tar" || strings.HasSuffix(name, ".tar"))
	if req.Extract && !isTar {
		return fmt.Errorf("extract requested but artifact extension %q is not tar", artifact.Extension)
	}
	if isTar {
		if err := os.MkdirAll(target, 0o750); err != nil {
			return fmt.Errorf("create target directory: %w", err)
		}
		log.Info("extracting archive", "target", target)
		return extractTar(ctx, r, target)
	}

	info, err := os.Stat(target)
	if err == nil && info.IsDir() {
		target = filepath.Join(target, filepath.Base(name))
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return fmt.Errorf("create target directory: %w", err)
	}
	tmp := target + ".partial"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create target file: %w", err)
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("write target file: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close target file: %w", err)
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("install target file: %w", err)
	}
	log.Info("artifact restored", "target", target)
	return nil
}

func extractTar(ctx context.Context, r io.Reader, target string) error {
	root, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolve target directory: %w", err)
	}
	tr := tar.NewReader(r)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read archive: %w", err)
		}
		path, err := safeJoin(root, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, os.FileMode(header.Mode)&0o777|0o700); err != nil {
				return fmt.Errorf("create directory %s: %w", header.Name, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
				return fmt.Errorf("create directory for %s: %w", header.Name, err)
			}
			f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(header.Mode)&0o777|0o600)
			if err != nil {
				return fmt.Errorf("create file %s: %w", header.Name, err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return fmt.Errorf("write file %s: %w", header.Name, err)
			}
			if err := f.Close(); err != nil {
				return fmt.Errorf("close file %s: %w", header.Name, err)
			}
		case tar.TypeSymlink:
			if _, err := safeJoin(root, filepath.Join(filepath.Dir(header.Name), header.Linkname)); err != nil {
				return fmt.Errorf("refusing symlink %s pointing outside the target directory", header.Name)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
				return fmt.Errorf("create directory for %s: %w", header.Name, err)
			}
			_ = os.Remove(path)
			if err := os.Symlink(header.Linkname, path); err != nil {
				return fmt.Errorf("create symlink %s: %w", header.Name, err)
			}
		default:
			continue
		}
	}
}

func safeJoin(root, name string) (string, error) {
	if strings.HasPrefix(name, "/") || strings.Contains(name, "\x00") {
		return "", fmt.Errorf("refusing unsafe archive entry %q", name)
	}
	clean := filepath.Clean(filepath.Join(root, name))
	if clean != root && !strings.HasPrefix(clean, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("refusing archive entry %q outside the target directory", name)
	}
	return clean, nil
}

func (e *Engine) restoreToSource(ctx context.Context, r io.Reader, req RestoreRequest, artifact core.Artifact, log *slog.Logger) error {
	sourceID := req.TargetSourceID
	if sourceID == "" {
		if artifact.JobID == "" {
			return errors.New("artifact has no job, targetSourceId is required")
		}
		job, err := e.store.Jobs.Get(ctx, artifact.JobID)
		if err != nil {
			return fmt.Errorf("load job: %w", err)
		}
		sourceID = job.SourceID
	}
	src, err := e.store.Sources.Get(ctx, sourceID)
	if err != nil {
		return fmt.Errorf("load source: %w", err)
	}
	if artifact.SourceKind != "" && src.Kind != artifact.SourceKind {
		return fmt.Errorf("source kind mismatch: artifact was created by %s, target source is %s", artifact.SourceKind, src.Kind)
	}
	driver, cfg, err := e.SourceRuntime(ctx, src)
	if err != nil {
		return err
	}
	restorer, ok := driver.(source.Restorer)
	if !ok || !driver.Spec().Has(core.CapRestore) {
		return fmt.Errorf("source driver %s does not support restore", src.Kind)
	}
	opts := source.RestoreOptions{
		Extension:  artifact.Extension,
		Size:       artifact.Size,
		Overwrite:  true,
		TargetPath: req.TargetPath,
		Params:     req.Params,
	}
	if err := restorer.Restore(ctx, cfg, r, opts, log); err != nil {
		return fmt.Errorf("restore into source %s: %w", src.Name, err)
	}
	return nil
}
