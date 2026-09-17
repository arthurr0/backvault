package ntfy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/notify/internal/testevent"
)

func capture(t *testing.T) (*httptest.Server, func() (map[string]any, http.Header)) {
	t.Helper()
	var mu sync.Mutex
	var body []byte
	var headers http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		body = b
		headers = r.Header.Clone()
		mu.Unlock()
		_, _ = w.Write([]byte("{}"))
	}))
	t.Cleanup(srv.Close)
	return srv, func() (map[string]any, http.Header) {
		mu.Lock()
		defer mu.Unlock()
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("payload is not json: %v (%s)", err, body)
		}
		return payload, headers
	}
}

func TestValidate(t *testing.T) {
	if err := New().Validate(core.Config{}); err == nil {
		t.Error("topic should be required")
	}
	if err := New().Validate(core.Config{"topic": "with space"}); err == nil {
		t.Error("topic with a space should be rejected")
	}
	if err := New().Validate(core.Config{"topic": "ok", "server_url": "::::"}); err == nil {
		t.Error("invalid server url should be rejected")
	}
}

func TestSendUsesPriorityBySeverity(t *testing.T) {
	srv, read := capture(t)
	if err := New().Send(t.Context(), core.Config{
		"server_url": srv.URL, "topic": "backvault", "token": "tk_secret", "tags": []string{"floppy_disk"},
	}, testevent.Failed(), testevent.Logger()); err != nil {
		t.Fatalf("send: %v", err)
	}
	payload, headers := read()
	if payload["topic"] != "backvault" {
		t.Errorf("topic %v", payload["topic"])
	}
	if int(payload["priority"].(float64)) != 4 {
		t.Errorf("priority %v, want 4", payload["priority"])
	}
	if headers.Get("Authorization") != "Bearer tk_secret" {
		t.Errorf("authorization header %q", headers.Get("Authorization"))
	}
	tags := payload["tags"].([]any)
	if len(tags) != 2 || tags[0] != "rotating_light" || tags[1] != "floppy_disk" {
		t.Errorf("tags %v", tags)
	}
	if payload["click"] != "https://backvault.example.com/runs/run1" {
		t.Errorf("click %v", payload["click"])
	}
	body := payload["message"].(string)
	for _, want := range []string{"Acme Backups", "Database nightly", "failed", "Hetzner Box", "could not connect"} {
		if !strings.Contains(body, want) {
			t.Errorf("message is missing %q:\n%s", want, body)
		}
	}
}

func TestSuccessUsesLowPriority(t *testing.T) {
	srv, read := capture(t)
	if err := New().Send(t.Context(), core.Config{"server_url": srv.URL, "topic": "backvault"},
		testevent.Success(), testevent.Logger()); err != nil {
		t.Fatalf("send: %v", err)
	}
	payload, _ := read()
	if int(payload["priority"].(float64)) != 2 {
		t.Errorf("priority %v, want 2", payload["priority"])
	}
}
