package discord

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

func capture(t *testing.T) (*httptest.Server, func() map[string]any) {
	t.Helper()
	var mu sync.Mutex
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		body = b
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
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
	if err := New().Validate(core.Config{}); err == nil {
		t.Error("webhook url should be required")
	}
	if err := New().Validate(core.Config{"webhook_url": "nope"}); err == nil {
		t.Error("invalid url should be rejected")
	}
}

func TestSendBuildsAnEmbed(t *testing.T) {
	srv, payload := capture(t)
	if err := New().Send(t.Context(), core.Config{"webhook_url": srv.URL},
		testevent.Failed(), testevent.Logger()); err != nil {
		t.Fatalf("send: %v", err)
	}
	embeds, ok := payload()["embeds"].([]any)
	if !ok || len(embeds) != 1 {
		t.Fatalf("expected one embed, got %v", payload()["embeds"])
	}
	e := embeds[0].(map[string]any)
	if int(e["color"].(float64)) != 0xD9483B {
		t.Errorf("colour %v", e["color"])
	}
	if e["url"] != "https://backvault.example.com/runs/run1" {
		t.Errorf("url %v", e["url"])
	}
	fields := e["fields"].([]any)
	seen := map[string]string{}
	for _, f := range fields {
		m := f.(map[string]any)
		seen[m["name"].(string)] = m["value"].(string)
	}
	for _, want := range []string{"Site", "Job", "Status", "Duration", "Size", "Destinations", "Error"} {
		if _, ok := seen[want]; !ok {
			t.Errorf("field %q is missing, got %v", want, seen)
		}
	}
	if !strings.Contains(e["timestamp"].(string), "2024-05-04") {
		t.Errorf("timestamp %v", e["timestamp"])
	}
}
