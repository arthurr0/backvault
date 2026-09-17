package message

import (
	"strings"
	"testing"
	"time"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/notify"
)

func TestBuildCollectsEveryRequiredField(t *testing.T) {
	started := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	ev := notify.Event{
		Type:     core.EventRunWarning,
		Severity: notify.SeverityWarning,
		SiteName: "Acme",
		BaseURL:  "https://backvault.example.com/",
		Job:      &core.Job{Slug: "nightly", Name: "Nightly", DestinationNames: []string{"S3", "Box"}},
		Run: &core.Run{
			ID: "r1", Kind: core.RunBackup, Status: core.RunWarning,
			StartedAt: &started, DurationMS: 65000, Bytes: 2 * 1024 * 1024, RawBytes: 9 * 1024 * 1024,
			Error: "one destination failed",
		},
	}
	m := Build(ev)
	want := map[string]string{
		"Site":         "Acme",
		"Job":          "Nightly",
		"Status":       "warning",
		"Duration":     "1m 05s",
		"Size":         "2.0 MiB",
		"Raw size":     "9.0 MiB",
		"Destinations": "S3, Box",
	}
	got := map[string]string{}
	for _, f := range m.Fields {
		got[f.Key] = f.Value
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("field %s = %q, want %q", k, got[k], v)
		}
	}
	if m.Error != "one destination failed" {
		t.Errorf("error %q", m.Error)
	}
	if m.Link != "https://backvault.example.com/runs/r1" {
		t.Errorf("link %q", m.Link)
	}
	if m.Color != ColorWarning {
		t.Errorf("colour %q", m.Color)
	}
	if !strings.Contains(m.Title, "Acme") || !strings.Contains(m.Title, "Nightly") {
		t.Errorf("title %q", m.Title)
	}
}

func TestColoursByEventType(t *testing.T) {
	cases := map[string]string{
		core.EventRunSuccess:      ColorSuccess,
		core.EventRunWarning:      ColorWarning,
		core.EventRunFailed:       ColorDanger,
		core.EventJobOverdue:      ColorWarning,
		core.EventRestoreDone:     ColorSuccess,
		core.EventRestoreFailed:   ColorDanger,
		core.EventArtifactMissing: ColorWarning,
		core.EventPruneDone:       ColorSuccess,
	}
	for eventType, want := range cases {
		m := Build(notify.Event{Type: eventType})
		if m.Color != want {
			t.Errorf("%s colour %q, want %q", eventType, m.Color, want)
		}
	}
}

func TestTextAndHTMLContainEverything(t *testing.T) {
	m := Build(notify.Event{
		Type: core.EventRunFailed, Severity: notify.SeverityError, SiteName: "Acme",
		Job:  &core.Job{Name: "Nightly", Slug: "nightly"},
		Run:  &core.Run{ID: "r1", Status: core.RunFailed, DurationMS: 1500, Bytes: 1024, Error: "boom & <fail>"},
		Link: "https://example.com/runs/r1",
	})
	text := m.Text()
	for _, want := range []string{"Acme", "Nightly", "failed", "boom & <fail>", "https://example.com/runs/r1"} {
		if !strings.Contains(text, want) {
			t.Errorf("text is missing %q:\n%s", want, text)
		}
	}
	html := m.HTML()
	if !strings.Contains(html, "boom &amp; &lt;fail&gt;") {
		t.Errorf("html did not escape the error:\n%s", html)
	}
	if !strings.Contains(html, "https://example.com/runs/r1") {
		t.Error("html is missing the link")
	}
}

func TestBytesAndDuration(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{{512, "512 B"}, {1536, "1.5 KiB"}, {5 * 1024 * 1024, "5.0 MiB"}, {3 * 1024 * 1024 * 1024, "3.0 GiB"}}
	for _, c := range cases {
		if got := Bytes(c.n); got != c.want {
			t.Errorf("Bytes(%d) = %q, want %q", c.n, got, c.want)
		}
	}
	durations := []struct {
		ms   int64
		want string
	}{{250, "250 ms"}, {1500, "1.5 s"}, {65000, "1m 05s"}, {3900000, "1h 05m"}}
	for _, c := range durations {
		if got := Duration(c.ms); got != c.want {
			t.Errorf("Duration(%d) = %q, want %q", c.ms, got, c.want)
		}
	}
}

func TestFallbackSiteNameAndExtraFields(t *testing.T) {
	m := Build(notify.Event{
		Type:   core.EventJobOverdue,
		Fields: map[string]string{"expected_interval": "24h", "site": "ignored"},
	})
	if m.SiteName != "Backvault" {
		t.Errorf("site name %q", m.SiteName)
	}
	found := false
	for _, f := range m.Fields {
		if f.Key == "Expected interval" && f.Value == "24h" {
			found = true
		}
		if f.Key == "Site" && f.Value == "ignored" {
			t.Error("the site field from Fields should not override the event site name")
		}
	}
	if !found {
		t.Errorf("extra fields were dropped: %v", m.Fields)
	}
}
