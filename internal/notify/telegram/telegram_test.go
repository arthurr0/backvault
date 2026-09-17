package telegram

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/notify"
	"github.com/arthurr0/backvault/internal/notify/internal/testevent"
)

func capture(t *testing.T, status int) (*httptest.Server, func() (map[string]any, string)) {
	t.Helper()
	var mu sync.Mutex
	var body []byte
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		body = b
		path = r.URL.Path
		mu.Unlock()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	return srv, func() (map[string]any, string) {
		mu.Lock()
		defer mu.Unlock()
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("payload is not json: %v (%s)", err, body)
		}
		return payload, path
	}
}

func TestValidate(t *testing.T) {
	if err := New().Validate(core.Config{"chat_id": "1"}); err == nil {
		t.Error("bot token should be required")
	}
	if err := New().Validate(core.Config{"bot_token": "notatoken", "chat_id": "1"}); err == nil {
		t.Error("malformed token should be rejected")
	}
	if err := New().Validate(core.Config{"bot_token": "123:abc", "chat_id": "1"}); err != nil {
		t.Errorf("valid config rejected: %v", err)
	}
}

func TestSendUsesHTMLParseModeAndEscapes(t *testing.T) {
	srv, read := capture(t, http.StatusOK)
	ev := testevent.Failed()
	ev.Run.Error = "pg_dump failed: <script>alert(1)</script> & more"
	if err := New().Send(t.Context(), core.Config{
		"bot_token": "123456:ABCDEF", "chat_id": "-1001", "api_base": srv.URL,
	}, ev, testevent.Logger()); err != nil {
		t.Fatalf("send: %v", err)
	}
	payload, path := read()
	if path != "/bot123456:ABCDEF/sendMessage" {
		t.Errorf("path %q", path)
	}
	if payload["parse_mode"] != "HTML" {
		t.Errorf("parse mode %v", payload["parse_mode"])
	}
	if payload["chat_id"] != "-1001" {
		t.Errorf("chat id %v", payload["chat_id"])
	}
	text := payload["text"].(string)
	if strings.Contains(text, "<script>") {
		t.Errorf("html was not escaped:\n%s", text)
	}
	if !strings.Contains(text, "&lt;script&gt;") {
		t.Errorf("escaped error text is missing:\n%s", text)
	}
	for _, want := range []string{"Acme Backups", "Database nightly", "failed", "Hetzner Box", "backvault.example.com/runs/run1"} {
		if !strings.Contains(text, want) {
			t.Errorf("message is missing %q:\n%s", want, text)
		}
	}
}

func TestSendScrubsTheTokenFromErrors(t *testing.T) {
	srv, _ := capture(t, http.StatusUnauthorized)
	err := New().Send(t.Context(), core.Config{
		"bot_token": "123456:SUPERSECRETTOKEN", "chat_id": "1", "api_base": srv.URL,
	}, notify.Event{Type: core.EventRunFailed, Severity: notify.SeverityError}, testevent.Logger())
	if err == nil {
		t.Fatal("a 401 response should be an error")
	}
	if strings.Contains(err.Error(), "SUPERSECRETTOKEN") {
		t.Errorf("token leaked into the error: %v", err)
	}
}
