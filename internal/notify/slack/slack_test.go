package slack

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/notify/internal/message"
	"github.com/arthurr0/backvault/internal/notify/internal/testevent"
)

func capture(t *testing.T) (*httptest.Server, func() map[string]any) {
	t.Helper()
	var mu sync.Mutex
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		body = b
		mu.Unlock()
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)
	return srv, func() map[string]any {
		mu.Lock()
		defer mu.Unlock()
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("payload is not json: %v (%s)", err, body)
		}
		return payload
	}
}

func TestValidate(t *testing.T) {
	d := New()
	if err := d.Validate(core.Config{}); err == nil {
		t.Error("webhook url should be required")
	}
	if err := d.Validate(core.Config{"webhook_url": "nonsense"}); err == nil {
		t.Error("invalid url should be rejected")
	}
}

func TestSendBuildsAColouredAttachment(t *testing.T) {
	srv, payload := capture(t)
	ev := testevent.Failed()
	if err := New().Send(t.Context(), core.Config{"webhook_url": srv.URL, "username": "Backvault"},
		ev, testevent.Logger()); err != nil {
		t.Fatalf("send: %v", err)
	}
	p := payload()
	if p["username"] != "Backvault" {
		t.Errorf("username %v", p["username"])
	}
	attachments, ok := p["attachments"].([]any)
	if !ok || len(attachments) != 1 {
		t.Fatalf("expected one attachment, got %v", p["attachments"])
	}
	att := attachments[0].(map[string]any)
	if att["color"] != message.ColorDanger {
		t.Errorf("colour %v, want %v", att["color"], message.ColorDanger)
	}
	if !strings.Contains(att["title"].(string), "Database nightly") {
		t.Errorf("title %v", att["title"])
	}
	if att["title_link"] != "https://backvault.example.com/runs/run1" {
		t.Errorf("link %v", att["title_link"])
	}
	fields := att["fields"].([]any)
	seen := map[string]string{}
	for _, f := range fields {
		m := f.(map[string]any)
		seen[m["title"].(string)] = m["value"].(string)
	}
	for _, want := range []string{"Site", "Job", "Status", "Duration", "Size", "Destinations", "Error"} {
		if _, ok := seen[want]; !ok {
			t.Errorf("field %q is missing, got %v", want, seen)
		}
	}
	if seen["Site"] != "Acme Backups" {
		t.Errorf("site %q", seen["Site"])
	}
	if seen["Destinations"] != "Hetzner Box, MinIO" {
		t.Errorf("destinations %q", seen["Destinations"])
	}
	if !strings.Contains(seen["Error"], "could not connect") {
		t.Errorf("error %q", seen["Error"])
	}
}

func TestSuccessIsGreen(t *testing.T) {
	srv, payload := capture(t)
	if err := New().Send(t.Context(), core.Config{"webhook_url": srv.URL},
		testevent.Success(), testevent.Logger()); err != nil {
		t.Fatalf("send: %v", err)
	}
	att := payload()["attachments"].([]any)[0].(map[string]any)
	if att["color"] != message.ColorSuccess {
		t.Errorf("colour %v, want %v", att["color"], message.ColorSuccess)
	}
}
