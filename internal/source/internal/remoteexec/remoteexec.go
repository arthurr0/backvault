package remoteexec

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/remote"
	"github.com/arthurr0/backvault/internal/source/internal/procstream"
)

const (
	defaultShell = "/bin/sh"
	scanMaxLine  = 64 * 1024
)

type Command struct {
	Argv    []string
	Script  string
	Shell   string
	Env     []string
	Dir     string
	Stdin   io.Reader
	Redact  []string
	Label   string
	Quiet   bool
	Cleanup []func()
}

type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

func (r Result) Trimmed() string { return strings.TrimSpace(r.Stdout) }

func (r Result) Detail() string {
	detail := strings.TrimSpace(r.Stderr)
	if detail == "" {
		detail = strings.TrimSpace(r.Stdout)
	}
	if len(detail) > 500 {
		detail = detail[:500]
	}
	return strings.Join(strings.Fields(detail), " ")
}

func IsRemote(cfg core.Config) bool { return cfg.Host() != nil }

func HostLabel(cfg core.Config) string {
	h := cfg.Host()
	if h == nil {
		return ""
	}
	return hostLabel(*h)
}

func hostLabel(h core.Host) string {
	if name := strings.TrimSpace(h.Name); name != "" {
		return name
	}
	return strings.TrimSpace(h.Address)
}

func ValidateRequired(spec core.DriverSpec, cfg core.Config) error {
	if cfg.Host() == nil {
		return core.ValidateRequired(spec, cfg)
	}
	filtered := spec
	filtered.Fields = make([]core.Field, 0, len(spec.Fields))
	for _, f := range spec.Fields {
		if f.LocalOnly {
			continue
		}
		filtered.Fields = append(filtered.Fields, f)
	}
	return core.ValidateRequired(filtered, cfg)
}

func EnvPairs(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		if k == "" {
			continue
		}
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}

func TempFileScript(content, inner string) string {
	return "umask 077; f=$(mktemp) || exit 1; printf '%s' " + remote.ShellQuote(content) +
		" > \"$f\"; " + inner + "; rc=$?; rm -f \"$f\"; exit $rc"
}

func ScratchScript(inner string) string {
	return "umask 077; f=$(mktemp) || exit 1; " + inner + "; rc=$?; rm -f \"$f\"; exit $rc"
}

func normalize(c Command) Command {
	if c.Script != "" && c.Shell == "" && len(c.Env) > 0 {
		c.Shell = defaultShell
	}
	return c
}

func ShellLine(c Command) string {
	c = normalize(c)
	var b strings.Builder
	if dir := strings.TrimSpace(c.Dir); dir != "" {
		b.WriteString("cd " + remote.ShellQuote(dir) + " && ")
	}
	if len(c.Env) > 0 {
		b.WriteString("env")
		for _, e := range c.Env {
			b.WriteString(" " + remote.ShellQuote(e))
		}
		b.WriteString(" ")
	}
	switch {
	case c.Script != "" && c.Shell != "":
		b.WriteString(remote.ShellJoin([]string{c.Shell, "-c", c.Script}))
	case c.Script != "":
		b.WriteString(c.Script)
	default:
		b.WriteString(remote.ShellJoin(c.Argv))
	}
	return b.String()
}

func LogLine(c Command) string {
	masked := c
	if len(c.Env) > 0 {
		masked.Env = make([]string, 0, len(c.Env))
		for _, e := range c.Env {
			if i := strings.Index(e, "="); i > 0 {
				masked.Env = append(masked.Env, e[:i]+"=***")
				continue
			}
			masked.Env = append(masked.Env, e)
		}
	}
	return procstream.Scrub(ShellLine(masked), c.Redact)
}

func label(c Command) string {
	if c.Label != "" {
		return c.Label
	}
	if len(c.Argv) > 0 {
		return baseName(c.Argv[0])
	}
	return "command"
}

func baseName(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

func toProc(c Command) procstream.Command {
	c = normalize(c)
	name := ""
	var args []string
	if c.Script != "" {
		name = c.Shell
		if name == "" {
			name = defaultShell
		}
		args = []string{"-c", c.Script}
	} else if len(c.Argv) > 0 {
		name = c.Argv[0]
		args = c.Argv[1:]
	}
	return procstream.Command{
		Name: name, Args: args, Env: c.Env, Dir: c.Dir, Stdin: c.Stdin,
		Redact: c.Redact, Label: label(c), Quiet: c.Quiet, Cleanup: c.Cleanup,
	}
}

type Runner struct {
	host   *core.Host
	client *remote.Client
	log    *slog.Logger
	mu     sync.Mutex
	closed bool
	handed bool
}

func Open(ctx context.Context, cfg core.Config, log *slog.Logger) (*Runner, error) {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	h := cfg.Host()
	if h == nil {
		return &Runner{log: log}, nil
	}
	client, err := remote.Dial(ctx, *h, log)
	if err != nil {
		return nil, fmt.Errorf("host %s: %w", hostLabel(*h), err)
	}
	return &Runner{host: h, client: client, log: log}, nil
}

func (r *Runner) Remote() bool { return r != nil && r.client != nil }

func (r *Runner) HostLabel() string {
	if r == nil || r.host == nil {
		return ""
	}
	return hostLabel(*r.host)
}

func (r *Runner) Log() *slog.Logger { return r.log }

func (r *Runner) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.handed || r.closed || r.client == nil {
		return nil
	}
	r.closed = true
	return r.client.Close()
}

func (r *Runner) release() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.client == nil {
		return
	}
	r.closed = true
	_ = r.client.Close()
}

func (r *Runner) scrub(text string) string {
	if r.host != nil {
		text = remote.ScrubText(*r.host, text)
	}
	return text
}

func (r *Runner) announce(c Command) {
	if c.Quiet {
		return
	}
	line := LogLine(c)
	if r.host != nil {
		line = remote.Wrap(*r.host, line)
	}
	r.log.Info("running "+label(c)+" on "+r.HostLabel(), "host", r.HostLabel(), "command", r.scrub(line))
}

func (r *Runner) Exec(ctx context.Context, c Command) (Result, error) {
	if !r.Remote() {
		res, err := localExec(ctx, c)
		runCleanup(c.Cleanup)
		return res, err
	}
	r.announce(c)
	out, err := r.client.WithRedact(c.Redact...).Exec(ctx, ShellLine(c), c.Stdin)
	runCleanup(c.Cleanup)
	res := Result{
		Stdout:   out.Stdout,
		Stderr:   procstream.Scrub(r.scrub(out.Stderr), c.Redact),
		ExitCode: out.ExitCode,
	}
	if err != nil {
		return res, fmt.Errorf("host %s: %w", r.HostLabel(), err)
	}
	return res, nil
}

func (r *Runner) Check(ctx context.Context, c Command, what string) error {
	res, err := r.Exec(ctx, c)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		if detail := res.Detail(); detail != "" {
			return fmt.Errorf("%s: %s", what, detail)
		}
		return fmt.Errorf("%s exited with code %d", what, res.ExitCode)
	}
	return nil
}

func (r *Runner) Start(ctx context.Context, c Command) (io.ReadCloser, error) {
	if !r.Remote() {
		return procstream.Start(ctx, r.log, toProc(c))
	}
	r.announce(c)
	st, err := r.client.WithRedact(c.Redact...).Start(ctx, ShellLine(c), c.Stdin, label(c))
	if err != nil {
		runCleanup(c.Cleanup)
		return nil, fmt.Errorf("host %s: %w", r.HostLabel(), err)
	}
	r.mu.Lock()
	r.handed = true
	r.mu.Unlock()
	return &stream{st: st, runner: r, cleanup: c.Cleanup}, nil
}

func (r *Runner) Run(ctx context.Context, c Command) error {
	if !r.Remote() {
		return procstream.Run(ctx, r.log, toProc(c))
	}
	rc, err := r.Start(ctx, c)
	if err != nil {
		return err
	}
	lines := bufio.NewScanner(rc)
	lines.Buffer(make([]byte, 0, 8192), scanMaxLine)
	for lines.Scan() {
		line := strings.TrimRight(lines.Text(), "\r")
		if strings.TrimSpace(line) == "" || c.Quiet {
			continue
		}
		r.log.Info(procstream.Scrub(r.scrub(line), c.Redact), "host", r.HostLabel())
	}
	return rc.Close()
}

func (r *Runner) Output(ctx context.Context, c Command) ([]byte, error) {
	if !r.Remote() {
		return procstream.Output(ctx, r.log, toProc(c))
	}
	res, err := r.Exec(ctx, c)
	if err != nil {
		return []byte(res.Stdout), err
	}
	if res.ExitCode != 0 {
		if detail := res.Detail(); detail != "" {
			return []byte(res.Stdout), fmt.Errorf("%s exited with code %d: %s", label(c), res.ExitCode, detail)
		}
		return []byte(res.Stdout), fmt.Errorf("%s exited with code %d", label(c), res.ExitCode)
	}
	return []byte(res.Stdout), nil
}

func (r *Runner) HasTool(ctx context.Context, name string) (bool, error) {
	if !r.Remote() {
		_, err := exec.LookPath(name)
		return err == nil, nil
	}
	res, err := r.Exec(ctx, Command{
		Script: "command -v " + remote.ShellQuote(name) + " >/dev/null 2>&1",
		Label:  "command -v", Quiet: true,
	})
	if err != nil {
		return false, err
	}
	return res.ExitCode == 0, nil
}

func (r *Runner) Tool(ctx context.Context, binaryPath string, candidates ...string) (string, error) {
	if !r.Remote() {
		return procstream.Lookup(binaryPath, candidates...)
	}
	if len(candidates) == 0 {
		return "", errors.New("no binary candidates given")
	}
	res, err := r.Exec(ctx, Command{
		Script: LookupScript(binaryPath, candidates), Label: "command -v", Quiet: true,
	})
	if err != nil {
		return "", err
	}
	if res.ExitCode != 0 || res.Trimmed() == "" {
		return "", fmt.Errorf("%s is not installed on host %s, install it or set the binary path field",
			strings.Join(candidates, " or "), r.HostLabel())
	}
	return res.Trimmed(), nil
}

func LookupScript(binaryPath string, candidates []string) string {
	names := remote.ShellJoin(candidates)
	var b strings.Builder
	if p := strings.TrimSpace(binaryPath); p != "" {
		quoted := remote.ShellQuote(p)
		b.WriteString("p=" + quoted + "; ")
		b.WriteString("if [ -d \"$p\" ]; then for n in " + names + "; do if [ -x \"$p/$n\" ]; then echo \"$p/$n\"; exit 0; fi; done; ")
		b.WriteString("elif [ -x \"$p\" ]; then echo \"$p\"; exit 0; fi; ")
	}
	b.WriteString("for n in " + names + "; do c=$(command -v \"$n\" 2>/dev/null) && { echo \"$c\"; exit 0; }; done; exit 1")
	return b.String()
}

func HasTool(ctx context.Context, cfg core.Config, name string) (bool, error) {
	r, err := Open(ctx, cfg, nil)
	if err != nil {
		return false, err
	}
	defer r.Close()
	return r.HasTool(ctx, name)
}

type stream struct {
	st      *remote.Stream
	runner  *Runner
	cleanup []func()
	once    sync.Once
	err     error
}

func (s *stream) Read(b []byte) (int, error) { return s.st.Read(b) }

func (s *stream) Close() error {
	s.once.Do(func() {
		s.err = s.st.Close()
		s.runner.release()
		runCleanup(s.cleanup)
	})
	return s.err
}

func localExec(ctx context.Context, c Command) (Result, error) {
	p := toProc(c)
	if p.Name == "" {
		return Result{}, errors.New("no command to run")
	}
	cmd := exec.CommandContext(ctx, p.Name, p.Args...)
	if len(p.Env) > 0 {
		cmd.Env = append(os.Environ(), p.Env...)
	}
	cmd.Dir = p.Dir
	cmd.Stdin = p.Stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := Result{
		Stdout: stdout.String(),
		Stderr: procstream.Scrub(stderr.String(), c.Redact),
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		res.ExitCode = exitErr.ExitCode()
		return res, nil
	}
	if err != nil {
		return res, fmt.Errorf("run %s: %w", p.Label, err)
	}
	return res, nil
}

func runCleanup(fns []func()) {
	for i := len(fns) - 1; i >= 0; i-- {
		if fns[i] != nil {
			fns[i]()
		}
	}
}
