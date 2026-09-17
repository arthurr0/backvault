package engine

import (
	"fmt"
	"io"
	"log/slog"
	"sync/atomic"
	"time"
)

type progressReader struct {
	r        io.Reader
	total    int64
	done     atomic.Int64
	started  time.Time
	last     time.Time
	interval time.Duration
	log      *slog.Logger
	label    string
}

func newProgressReader(r io.Reader, total int64, interval time.Duration, log *slog.Logger, label string) *progressReader {
	now := time.Now()
	return &progressReader{r: r, total: total, started: now, last: now, interval: interval, log: log, label: label}
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		done := p.done.Add(int64(n))
		if now := time.Now(); now.Sub(p.last) >= p.interval {
			p.last = now
			p.report(done, now)
		}
	}
	return n, err
}

func (p *progressReader) Done() int64 {
	return p.done.Load()
}

func (p *progressReader) Rate(now time.Time) float64 {
	elapsed := now.Sub(p.started).Seconds()
	if elapsed <= 0 {
		return 0
	}
	return float64(p.done.Load()) / elapsed
}

func (p *progressReader) report(done int64, now time.Time) {
	attrs := []any{"destination", p.label, "sent", formatBytes(done), "rate", formatBytes(int64(p.Rate(now))) + "/s"}
	if p.total > 0 {
		attrs = append(attrs, "total", formatBytes(p.total), "percent", fmt.Sprintf("%.1f", float64(done)*100/float64(p.total)))
		if rate := p.Rate(now); rate > 0 && done < p.total {
			attrs = append(attrs, "eta", (time.Duration(float64(p.total-done)/rate) * time.Second).Round(time.Second).String())
		}
	}
	p.log.Info("upload progress", attrs...)
}
