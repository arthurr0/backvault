package files

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/remote"
	"github.com/arthurr0/backvault/internal/source"
	"github.com/arthurr0/backvault/internal/source/internal/remoteexec"
)

func remoteBase(cfg core.Config) string {
	base := strings.TrimSpace(cfg.String("base_dir"))
	if base == "" {
		return "/"
	}
	return filepath.Clean(base)
}

func remotePaths(cfg core.Config) []string {
	base := remoteBase(cfg)
	out := make([]string, 0, len(cfg.StringList("paths")))
	for _, p := range cfg.StringList("paths") {
		out = append(out, archivePath(base, filepath.Clean(p)))
	}
	return out
}

func archivePath(base, full string) string {
	if base == full {
		return "."
	}
	if !within(base, full) {
		return full
	}
	rel, err := filepath.Rel(base, full)
	if err != nil || rel == "" || rel == "." || strings.HasPrefix(rel, "..") {
		return full
	}
	return rel
}

func tarArgv(tool string, cfg core.Config) []string {
	argv := []string{tool, "-C", remoteBase(cfg), "-cf", "-"}
	if cfg.Bool("one_file_system", false) {
		argv = append(argv, "--one-file-system")
	}
	if cfg.Bool("follow_symlinks", false) {
		argv = append(argv, "-h")
	}
	for _, pattern := range cfg.StringList("exclude") {
		argv = append(argv, "--exclude="+pattern)
	}
	return append(argv, remotePaths(cfg)...)
}

func extractArgv(tool, target string, cfg core.Config) []string {
	argv := []string{tool, "-C", target, "-xf", "-"}
	if !cfg.Bool("restore_ownership", false) {
		argv = append(argv, "--no-same-owner")
	}
	return argv
}

func extractScript(tool, target string, cfg core.Config) string {
	return "mkdir -p " + remote.ShellQuote(target) + " && " + remote.ShellJoin(extractArgv(tool, target, cfg))
}

func readableScript(cfg core.Config) string {
	paths := make([]string, 0, len(cfg.StringList("paths")))
	for _, p := range cfg.StringList("paths") {
		paths = append(paths, filepath.Clean(p))
	}
	if base := strings.TrimSpace(cfg.String("base_dir")); base != "" {
		paths = append(paths, filepath.Clean(base))
	}
	return "for p in " + remote.ShellJoin(paths) + "; do [ -r \"$p\" ] || echo \"$p\"; done"
}

func (d *Driver) testRemote(ctx context.Context, cfg core.Config, log *slog.Logger) error {
	r, err := remoteexec.Open(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer r.Close()
	if _, err := r.Tool(ctx, "", "tar"); err != nil {
		return err
	}
	res, err := r.Exec(ctx, remoteexec.Command{Script: readableScript(cfg), Label: "test -r", Quiet: true})
	if err != nil {
		return err
	}
	var problems []string
	for _, line := range strings.Split(res.Stdout, "\n") {
		if p := strings.TrimSpace(line); p != "" {
			problems = append(problems, p)
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("unreadable on host %s: %s", r.HostLabel(), strings.Join(problems, ", "))
	}
	log.Info("all paths readable on the host", "host", r.HostLabel(), "paths", len(cfg.StringList("paths")))
	return nil
}

func (d *Driver) backupRemote(ctx context.Context, cfg core.Config, log *slog.Logger) (*source.Stream, error) {
	r, err := remoteexec.Open(ctx, cfg, log)
	if err != nil {
		return nil, err
	}
	tool, err := r.Tool(ctx, "", "tar")
	if err != nil {
		r.Close()
		return nil, err
	}
	log.Info("archiving paths on the host", "host", r.HostLabel(), "base", remoteBase(cfg),
		"paths", strings.Join(remotePaths(cfg), ", "))
	rc, err := r.Start(ctx, remoteexec.Command{Argv: tarArgv(tool, cfg), Label: "tar"})
	if err != nil {
		r.Close()
		return nil, err
	}
	return &source.Stream{
		Reader:    rc,
		Extension: "tar",
		Meta: map[string]string{
			"host":  r.HostLabel(),
			"paths": strings.Join(cfg.StringList("paths"), ", "),
		},
	}, nil
}

func (d *Driver) restoreRemote(ctx context.Context, cfg core.Config, src io.Reader, opts source.RestoreOptions, log *slog.Logger) error {
	target := opts.TargetPath
	if target == "" {
		target = cfg.String("base_dir")
	}
	if target == "" {
		return fmt.Errorf("a target directory is required to restore a file archive")
	}
	if !filepath.IsAbs(target) {
		return fmt.Errorf("target directory must be an absolute path")
	}
	r, err := remoteexec.Open(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer r.Close()
	tool, err := r.Tool(ctx, "", "tar")
	if err != nil {
		return err
	}
	log.Info("extracting archive on the host", "host", r.HostLabel(), "target", target)
	return r.Run(ctx, remoteexec.Command{
		Script: extractScript(tool, filepath.Clean(target), cfg), Stdin: src, Label: "tar",
	})
}
