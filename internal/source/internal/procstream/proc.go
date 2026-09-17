package procstream

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
	"strings"
	"sync"
	"time"
)

const (
	stderrTailLines = 20
	stderrMaxLine   = 64 * 1024
	waitDelay       = 15 * time.Second
	killGrace       = 3 * time.Second
)

type Command struct {
	Name    string
	Args    []string
	Env     []string
	Dir     string
	Stdin   io.Reader
	Redact  []string
	Label   string
	Quiet   bool
	Cleanup []func()
}

type Process struct {
	cmd     *exec.Cmd
	stdout  io.ReadCloser
	cancel  context.CancelFunc
	tail    *tail
	stderr  sync.WaitGroup
	once    sync.Once
	err     error
	eof     bool
	label   string
	cleanup []func()
}

func Start(ctx context.Context, log *slog.Logger, c Command) (*Process, error) {
	p, err := start(ctx, log, c)
	if err != nil {
		runCleanup(c.Cleanup)
		return nil, err
	}
	return p, nil
}

func Run(ctx context.Context, log *slog.Logger, c Command) error {
	p, err := start(ctx, log, c)
	if err != nil {
		runCleanup(c.Cleanup)
		return err
	}
	if p.stdout != nil {
		lines := bufio.NewScanner(p)
		lines.Buffer(make([]byte, 0, 8192), stderrMaxLine)
		for lines.Scan() {
			line := strings.TrimRight(lines.Text(), "\r")
			if line != "" && !c.Quiet {
				log.Info(Scrub(line, c.Redact), "tool", p.label)
			}
		}
	}
	return p.Close()
}

func Output(ctx context.Context, log *slog.Logger, c Command) ([]byte, error) {
	p, err := start(ctx, log, c)
	if err != nil {
		runCleanup(c.Cleanup)
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(p, 4*1024*1024))
	closeErr := p.Close()
	if readErr != nil {
		return data, readErr
	}
	return data, closeErr
}

func start(ctx context.Context, log *slog.Logger, c Command) (*Process, error) {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	label := c.Label
	if label == "" {
		label = baseName(c.Name)
	}
	runCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(runCtx, c.Name, c.Args...)
	cmd.WaitDelay = waitDelay
	cmd.Cancel = func() error { return terminate(cmd) }
	setProcessGroup(cmd)
	cmd.Dir = c.Dir
	if len(c.Env) > 0 {
		cmd.Env = append(os.Environ(), c.Env...)
	}
	if c.Stdin != nil {
		cmd.Stdin = c.Stdin
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("stdout pipe for %s: %w", label, err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("stderr pipe for %s: %w", label, err)
	}
	p := &Process{
		cmd:     cmd,
		stdout:  stdout,
		cancel:  cancel,
		tail:    newTail(stderrTailLines),
		label:   label,
		cleanup: c.Cleanup,
	}
	log.Info("running "+label, "command", Scrub(shellQuote(c.Name, c.Args), c.Redact))
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start %s: %w", label, err)
	}
	p.stderr.Add(1)
	go func() {
		defer p.stderr.Done()
		lines := bufio.NewScanner(stderr)
		lines.Buffer(make([]byte, 0, 8192), stderrMaxLine)
		for lines.Scan() {
			line := strings.TrimRight(lines.Text(), "\r")
			if strings.TrimSpace(line) == "" {
				continue
			}
			line = Scrub(line, c.Redact)
			p.tail.add(line)
			if !c.Quiet {
				if looksLikeError(line) {
					log.Warn(line, "tool", label)
				} else {
					log.Info(line, "tool", label)
				}
			}
		}
	}()
	return p, nil
}

func (p *Process) Read(b []byte) (int, error) {
	n, err := p.stdout.Read(b)
	if errors.Is(err, io.EOF) {
		p.eof = true
	}
	return n, err
}

func (p *Process) Close() error {
	p.once.Do(func() {
		aborted := !p.eof
		if aborted {
			p.cancel()
		}
		_ = p.stdout.Close()
		p.stderr.Wait()
		waitErr := p.cmd.Wait()
		p.cancel()
		runCleanup(p.cleanup)
		detail := p.tail.text()
		if aborted {
			if detail != "" {
				p.err = fmt.Errorf("%s was stopped before it finished: %s", p.label, detail)
			} else {
				p.err = fmt.Errorf("%s was stopped before it finished", p.label)
			}
			return
		}
		if waitErr == nil {
			return
		}
		var exitErr *exec.ExitError
		switch {
		case errors.As(waitErr, &exitErr):
			if detail != "" {
				p.err = fmt.Errorf("%s exited with code %d: %s", p.label, exitErr.ExitCode(), detail)
			} else {
				p.err = fmt.Errorf("%s exited with code %d", p.label, exitErr.ExitCode())
			}
		default:
			if detail != "" {
				p.err = fmt.Errorf("%s failed: %w: %s", p.label, waitErr, detail)
			} else {
				p.err = fmt.Errorf("%s failed: %w", p.label, waitErr)
			}
		}
	})
	return p.err
}

func runCleanup(fns []func()) {
	for i := len(fns) - 1; i >= 0; i-- {
		if fns[i] != nil {
			fns[i]()
		}
	}
}

type tail struct {
	mu    sync.Mutex
	max   int
	lines []string
}

func newTail(max int) *tail {
	return &tail{max: max}
}

func (t *tail) add(line string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lines = append(t.lines, line)
	if len(t.lines) > t.max {
		t.lines = t.lines[len(t.lines)-t.max:]
	}
}

func (t *tail) text() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.Join(t.lines, " | ")
}

func Scrub(s string, redact []string) string {
	for _, r := range redact {
		if len(r) < 3 {
			continue
		}
		s = strings.ReplaceAll(s, r, "***")
	}
	return s
}

func shellQuote(name string, args []string) string {
	var b bytes.Buffer
	b.WriteString(quote(name))
	for _, a := range args {
		b.WriteByte(' ')
		b.WriteString(quote(a))
	}
	return b.String()
}

func quote(s string) string {
	if s == "" {
		return "''"
	}
	if strings.IndexFunc(s, func(r rune) bool {
		return r <= ' ' || strings.ContainsRune("\"'\\$`|&;<>()*?[]{}!#~", r)
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func baseName(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

func looksLikeError(line string) bool {
	lower := strings.ToLower(line)
	for _, marker := range []string{"error", "fatal", "failed", "denied", "refused", "cannot", "could not", "warning"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
