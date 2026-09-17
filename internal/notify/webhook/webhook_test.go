package webhook

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/notify"
	"github.com/arthurr0/backvault/internal/notify/internal/testevent"
)

type recorder struct {
	mu      sync.Mutex
	body    []byte
	headers http.Header
	method  string
	status  int
}

func serve(t *testing.T, rec *recorder) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		rec.body = body
		rec.headers = r.Header.Clone()
		rec.method = r.Method
		rec.mu.Unlock()
		if rec.status != 0 {
			w.WriteHeader(rec.status)
			_, _ = w.Write([]byte("rejected"))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestValidate(t *testing.T) {
	d := New()
	if err := d.Validate(core.Config{}); err == nil {
		t.Error("url should be required")
	}
	if err := d.Validate(core.Config{"url": "not a url"}); err == nil {
		t.Error("invalid url should be rejected")
	}
	if err := d.Validate(core.Config{"url": "ftp://example.com"}); err == nil {
		t.Error("non http scheme should be rejected")
	}
	if err := d.Validate(core.Config{"url": "https://example.com", "headers": []string{"broken"}}); err == nil {
		t.Error("malformed header should be rejected")
	}
	if err := d.Validate(core.Config{"url": "https://example.com", "method": "DELETE"}); err == nil {
		t.Error("unsupported method should be rejected")
	}
}

func TestSendPostsTheEventAsJSON(t *testing.T) {
	rec := &recorder{}
	srv := serve(t, rec)
	ev := testevent.Failed()
	err := New().Send(t.Context(), core.Config{
		"url":     srv.URL,
		"headers": []string{"X-Team: ops", "Authorization: Bearer abc"},
	}, ev, testevent.Logger())
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.method != http.MethodPost {
		t.Errorf("method %q", rec.method)
	}
	if got := rec.headers.Get("X-Team"); got != "ops" {
		t.Errorf("custom header %q", got)
	}
	if got := rec.headers.Get("Authorization"); got != "Bearer abc" {
		t.Errorf("authorization header %q", got)
	}
	if got := rec.headers.Get("X-Backvault-Event"); got != core.EventRunFailed {
		t.Errorf("event header %q", got)
	}
	if rec.headers.Get(SignatureHeader) != "" {
		t.Error("no signature should be sent without a secret")
	}
	var decoded notify.Event
	if err := json.Unmarshal(rec.body, &decoded); err != nil {
		t.Fatalf("body is not the event json: %v", err)
	}
	if decoded.Type != core.EventRunFailed {
		t.Errorf("event type %q", decoded.Type)
	}
	if decoded.Run == nil || decoded.Run.ID != "run1" {
		t.Error("run is missing from the payload")
	}
	if decoded.Job == nil || decoded.Job.Slug != "db-nightly" {
		t.Error("job is missing from the payload")
	}
	if decoded.SiteName != "Acme Backups" {
		t.Errorf("site name %q", decoded.SiteName)
	}
}

func TestSendSignsTheBody(t *testing.T) {
	rec := &recorder{}
	srv := serve(t, rec)
	err := New().Send(t.Context(), core.Config{"url": srv.URL, "secret": "topsecret"},
		testevent.Success(), testevent.Logger())
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	want := Sign("topsecret", rec.body)
	if got := rec.headers.Get(SignatureHeader); got != want {
		t.Errorf("signature %q, want %q", got, want)
	}
	if len(want) != 64 {
		t.Errorf("signature should be 64 hex characters, got %d", len(want))
	}
}

func TestSendReportsServerErrors(t *testing.T) {
	rec := &recorder{status: http.StatusInternalServerError}
	srv := serve(t, rec)
	err := New().Send(t.Context(), core.Config{"url": srv.URL}, testevent.Success(), testevent.Logger())
	if err == nil {
		t.Fatal("a 500 response should be reported as an error")
	}
}

func TestSendHonoursMethod(t *testing.T) {
	rec := &recorder{}
	srv := serve(t, rec)
	if err := New().Send(t.Context(), core.Config{"url": srv.URL, "method": "PUT"},
		testevent.Success(), testevent.Logger()); err != nil {
		t.Fatalf("send: %v", err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.method != http.MethodPut {
		t.Errorf("method %q, want PUT", rec.method)
	}
}
