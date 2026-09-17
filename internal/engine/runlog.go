package engine

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

const (
	logFlushInterval = 300 * time.Millisecond
	logFlushBytes    = 16 * 1024
	logMaxLine       = 8 * 1024
)

type logSink func(text string)

type runLog struct {
	mu     sync.Mutex
	buf    strings.Builder
	sink   logSink
	timer  *time.Timer
	closed bool
}

func newRunLog(sink logSink) *runLog {
	return &runLog{sink: sink}
}

func (l *runLog) write(line string) {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return
	}
	l.buf.WriteString(line)
	flushNow := l.buf.Len() >= logFlushBytes
	if !flushNow && l.timer == nil {
		l.timer = time.AfterFunc(logFlushInterval, l.Flush)
	}
	var pending string
	if flushNow {
		pending = l.buf.String()
		l.buf.Reset()
		if l.timer != nil {
			l.timer.Stop()
			l.timer = nil
		}
	}
	l.mu.Unlock()
	if pending != "" {
		l.sink(pending)
	}
}

func (l *runLog) Flush() {
	l.mu.Lock()
	if l.timer != nil {
		l.timer.Stop()
		l.timer = nil
	}
	pending := l.buf.String()
	l.buf.Reset()
	l.mu.Unlock()
	if pending != "" {
		l.sink(pending)
	}
}

func (l *runLog) Close() {
	l.Flush()
	l.mu.Lock()
	l.closed = true
	l.mu.Unlock()
}

type runLogHandler struct {
	log   *runLog
	level slog.Leveler
	attrs []slog.Attr
	group string
}

func newRunLogHandler(l *runLog, level slog.Leveler) slog.Handler {
	return &runLogHandler{log: l, level: level}
}

func (h *runLogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

func (h *runLogHandler) Handle(_ context.Context, rec slog.Record) error {
	var sb strings.Builder
	sb.WriteString(rec.Time.UTC().Format("15:04:05"))
	sb.WriteString(" ")
	sb.WriteString(levelLabel(rec.Level))
	sb.WriteString(" ")
	sb.WriteString(sanitizeLogText(rec.Message))
	for _, a := range h.attrs {
		writeAttr(&sb, h.group, a)
	}
	rec.Attrs(func(a slog.Attr) bool {
		writeAttr(&sb, h.group, a)
		return true
	})
	sb.WriteString("\n")
	text := sb.String()
	if len(text) > logMaxLine {
		text = text[:logMaxLine] + " [truncated]\n"
	}
	h.log.write(text)
	return nil
}

func (h *runLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := *h
	next.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &next
}

func (h *runLogHandler) WithGroup(name string) slog.Handler {
	next := *h
	if h.group == "" {
		next.group = name
	} else {
		next.group = h.group + "." + name
	}
	return &next
}

func writeAttr(sb *strings.Builder, group string, a slog.Attr) {
	a.Value = a.Value.Resolve()
	if a.Equal(slog.Attr{}) {
		return
	}
	key := a.Key
	if group != "" {
		key = group + "." + key
	}
	sb.WriteString(" ")
	sb.WriteString(sanitizeLogText(key))
	sb.WriteString("=")
	sb.WriteString(sanitizeLogText(fmt.Sprint(a.Value.Any())))
}

func levelLabel(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return "ERROR"
	case l >= slog.LevelWarn:
		return "WARN "
	case l >= slog.LevelInfo:
		return "INFO "
	default:
		return "DEBUG"
	}
}

func sanitizeLogText(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.Map(func(r rune) rune {
		if r == 0x1b {
			return -1
		}
		if r < 0x20 && r != '\t' {
			return ' '
		}
		return r
	}, s)
}
