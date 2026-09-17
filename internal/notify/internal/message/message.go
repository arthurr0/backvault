package message

import (
	"fmt"
	"html"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/notify"
)

const (
	ColorSuccess = "#2E9E6B"
	ColorWarning = "#E0A100"
	ColorDanger  = "#D9483B"
	ColorInfo    = "#3B82F6"
)

type KV struct {
	Key   string
	Value string
}

type Model struct {
	Title    string
	Summary  string
	Severity notify.Severity
	Status   string
	Fields   []KV
	Error    string
	Link     string
	Color    string
	Time     time.Time
	SiteName string
	JobName  string
}

var knownFieldKeys = map[string]bool{
	"site": true, "job": true, "status": true, "duration": true,
	"size": true, "destinations": true, "error": true, "link": true,
}

func Build(ev notify.Event) Model {
	m := Model{
		Severity: ev.Severity,
		Link:     link(ev),
		Time:     ev.Time,
		SiteName: siteName(ev),
		Error:    strings.TrimSpace(errorText(ev)),
	}
	if m.Time.IsZero() {
		m.Time = time.Now().UTC()
	}
	if ev.Job != nil {
		m.JobName = ev.Job.Name
	}
	if ev.Run != nil {
		m.Status = string(ev.Run.Status)
	}
	m.Color = color(ev)

	m.Fields = append(m.Fields, KV{"Site", m.SiteName})
	if m.JobName != "" {
		m.Fields = append(m.Fields, KV{"Job", m.JobName})
	}
	if m.Status != "" {
		m.Fields = append(m.Fields, KV{"Status", m.Status})
	} else {
		m.Fields = append(m.Fields, KV{"Event", ev.Type})
	}
	if ev.Run != nil {
		if ev.Run.Kind != "" {
			m.Fields = append(m.Fields, KV{"Run type", string(ev.Run.Kind)})
		}
		if ev.Run.DurationMS > 0 {
			m.Fields = append(m.Fields, KV{"Duration", Duration(ev.Run.DurationMS)})
		}
		if ev.Run.Bytes > 0 {
			m.Fields = append(m.Fields, KV{"Size", Bytes(ev.Run.Bytes)})
		}
		if ev.Run.RawBytes > 0 && ev.Run.RawBytes != ev.Run.Bytes {
			m.Fields = append(m.Fields, KV{"Raw size", Bytes(ev.Run.RawBytes)})
		}
	}
	if dests := destinations(ev); dests != "" {
		m.Fields = append(m.Fields, KV{"Destinations", dests})
	}
	extra := make([]KV, 0, len(ev.Fields))
	for k, v := range ev.Fields {
		if knownFieldKeys[strings.ToLower(k)] || strings.TrimSpace(v) == "" {
			continue
		}
		extra = append(extra, KV{Label(k), v})
	}
	sort.Slice(extra, func(i, j int) bool { return extra[i].Key < extra[j].Key })
	m.Fields = append(m.Fields, extra...)

	m.Title = strings.TrimSpace(ev.Title)
	if m.Title == "" {
		m.Title = defaultTitle(ev, m)
	}
	m.Summary = strings.TrimSpace(ev.Message)
	if m.Summary == "" {
		m.Summary = m.Title
	}
	return m
}

func defaultTitle(ev notify.Event, m Model) string {
	switch {
	case m.JobName != "" && m.Status != "":
		return fmt.Sprintf("%s: %s %s", m.SiteName, m.JobName, m.Status)
	case m.JobName != "":
		return fmt.Sprintf("%s: %s %s", m.SiteName, m.JobName, Label(ev.Type))
	default:
		return fmt.Sprintf("%s: %s", m.SiteName, Label(ev.Type))
	}
}

func siteName(ev notify.Event) string {
	if s := strings.TrimSpace(ev.SiteName); s != "" {
		return s
	}
	return "Backvault"
}

func errorText(ev notify.Event) string {
	if ev.Run != nil && ev.Run.Error != "" {
		return ev.Run.Error
	}
	if v, ok := ev.Fields["error"]; ok {
		return v
	}
	return ""
}

func destinations(ev notify.Event) string {
	if v, ok := ev.Fields["destinations"]; ok && strings.TrimSpace(v) != "" {
		return v
	}
	if ev.Job != nil && len(ev.Job.DestinationNames) > 0 {
		return strings.Join(ev.Job.DestinationNames, ", ")
	}
	return ""
}

func link(ev notify.Event) string {
	if l := strings.TrimSpace(ev.Link); l != "" {
		return l
	}
	base := strings.TrimRight(strings.TrimSpace(ev.BaseURL), "/")
	if base == "" {
		return ""
	}
	if ev.Run != nil && ev.Run.ID != "" {
		return base + "/runs/" + ev.Run.ID
	}
	if ev.Job != nil && ev.Job.Slug != "" {
		return base + "/jobs/" + ev.Job.Slug
	}
	return base
}

func color(ev notify.Event) string {
	switch ev.Type {
	case core.EventRunSuccess, core.EventPruneDone, core.EventRestoreDone:
		return ColorSuccess
	case core.EventRunWarning, core.EventJobOverdue, core.EventArtifactMissing:
		return ColorWarning
	case core.EventRunFailed, core.EventRestoreFailed:
		return ColorDanger
	}
	switch ev.Severity {
	case notify.SeverityError:
		return ColorDanger
	case notify.SeverityWarning:
		return ColorWarning
	default:
		return ColorInfo
	}
}

func (m Model) ColorInt() int {
	v, err := strconv.ParseInt(strings.TrimPrefix(m.Color, "#"), 16, 32)
	if err != nil {
		return 0x3B82F6
	}
	return int(v)
}

func (m Model) Text() string {
	var b strings.Builder
	b.WriteString(m.Title)
	b.WriteString("\n\n")
	if m.Summary != "" && m.Summary != m.Title {
		b.WriteString(m.Summary)
		b.WriteString("\n\n")
	}
	for _, f := range m.Fields {
		b.WriteString(f.Key)
		b.WriteString(": ")
		b.WriteString(f.Value)
		b.WriteString("\n")
	}
	if m.Error != "" {
		b.WriteString("\nError: ")
		b.WriteString(m.Error)
		b.WriteString("\n")
	}
	if m.Link != "" {
		b.WriteString("\n")
		b.WriteString(m.Link)
		b.WriteString("\n")
	}
	return b.String()
}

func (m Model) HTML() string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html><body style="margin:0;padding:24px;background:#F6F3EC;font-family:Inter,Segoe UI,Helvetica,Arial,sans-serif;color:#0F1720">`)
	b.WriteString(`<table role="presentation" cellpadding="0" cellspacing="0" style="max-width:600px;margin:0 auto;background:#FFFFFF;border:1px solid #E5E1D8;border-radius:10px;overflow:hidden">`)
	b.WriteString(`<tr><td style="height:4px;background:` + html.EscapeString(m.Color) + `"></td></tr>`)
	b.WriteString(`<tr><td style="padding:24px">`)
	b.WriteString(`<h1 style="margin:0 0 12px;font-size:18px;font-weight:600;letter-spacing:-0.01em">` + html.EscapeString(m.Title) + `</h1>`)
	if m.Summary != "" && m.Summary != m.Title {
		b.WriteString(`<p style="margin:0 0 16px;font-size:14px;color:#334155">` + html.EscapeString(m.Summary) + `</p>`)
	}
	b.WriteString(`<table role="presentation" cellpadding="0" cellspacing="0" style="width:100%;font-size:14px">`)
	for _, f := range m.Fields {
		b.WriteString(`<tr><td style="padding:4px 12px 4px 0;color:#64748B;white-space:nowrap;vertical-align:top">` +
			html.EscapeString(f.Key) + `</td><td style="padding:4px 0;color:#0F1720">` + html.EscapeString(f.Value) + `</td></tr>`)
	}
	b.WriteString(`</table>`)
	if m.Error != "" {
		b.WriteString(`<pre style="margin:16px 0 0;padding:12px;background:#F6F3EC;border-left:3px solid ` +
			html.EscapeString(m.Color) + `;font-family:JetBrains Mono,SFMono-Regular,Consolas,monospace;font-size:12px;white-space:pre-wrap;color:#0F1720">` +
			html.EscapeString(m.Error) + `</pre>`)
	}
	if m.Link != "" {
		b.WriteString(`<p style="margin:20px 0 0"><a href="` + html.EscapeString(m.Link) +
			`" style="display:inline-block;padding:9px 16px;background:#D4A032;color:#0F1720;text-decoration:none;border-radius:6px;font-weight:600;font-size:14px">Open in Backvault</a></p>`)
	}
	b.WriteString(`</td></tr></table>`)
	b.WriteString(`<p style="max-width:600px;margin:16px auto 0;font-size:12px;color:#94A3B8;text-align:center">Sent by Backvault. Every backup, accounted for.</p>`)
	b.WriteString(`</body></html>`)
	return b.String()
}

func (m Model) Subject() string {
	return m.Title
}

func Bytes(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit && exp < 4; v /= unit {
		div *= unit
		exp++
	}
	return strconv.FormatFloat(float64(n)/float64(div), 'f', 1, 64) + " " + [...]string{"KiB", "MiB", "GiB", "TiB", "PiB"}[exp]
}

func Duration(ms int64) string {
	d := time.Duration(ms) * time.Millisecond
	switch {
	case d < time.Second:
		return strconv.FormatInt(ms, 10) + " ms"
	case d < time.Minute:
		return strconv.FormatFloat(d.Seconds(), 'f', 1, 64) + " s"
	case d < time.Hour:
		return fmt.Sprintf("%dm %02ds", int(d.Minutes()), int(d.Seconds())%60)
	default:
		return fmt.Sprintf("%dh %02dm", int(d.Hours()), int(d.Minutes())%60)
	}
}

func Label(s string) string {
	s = strings.NewReplacer("_", " ", ".", " ", "-", " ").Replace(s)
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
