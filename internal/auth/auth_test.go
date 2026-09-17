package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/store"
)

func newManager(t *testing.T) (*Manager, *store.Store) {
	t.Helper()
	s, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "backvault.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return NewManager(s), s
}

func TestPasswordHashing(t *testing.T) {
	hash, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	ok, err := VerifyPassword(hash, "correct horse battery")
	if err != nil || !ok {
		t.Fatalf("verify: %v %v", ok, err)
	}
	ok, err = VerifyPassword(hash, "wrong")
	if err != nil || ok {
		t.Fatalf("expected mismatch: %v %v", ok, err)
	}
	if _, err := VerifyPassword("nonsense", "x"); !errors.Is(err, ErrInvalidHash) {
		t.Fatalf("expected invalid hash, got %v", err)
	}
}

func TestLoginAndSession(t *testing.T) {
	ctx := context.Background()
	m, _ := newManager(t)
	if _, err := m.CreateUser(ctx, core.User{Email: "a@b.c", Role: core.RoleAdmin}, "supersecret123"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Login(ctx, "a@b.c", "nope"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected invalid credentials, got %v", err)
	}
	if _, err := m.Login(ctx, "missing@b.c", "nope"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected invalid credentials, got %v", err)
	}
	u, err := m.Login(ctx, "a@b.c", "supersecret123")
	if err != nil {
		t.Fatal(err)
	}
	secret, expires, err := m.CreateSession(ctx, u.ID, "127.0.0.1", "go-test")
	if err != nil {
		t.Fatal(err)
	}
	if time.Until(expires) < 29*24*time.Hour {
		t.Fatalf("expiry too short: %v", expires)
	}
	p, err := m.ResolveSession(ctx, secret)
	if err != nil {
		t.Fatal(err)
	}
	if p.Type != AuthSession || !p.IsAdmin() {
		t.Fatalf("principal = %+v", p)
	}
	if err := m.DestroySession(ctx, secret); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ResolveSession(ctx, secret); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestTokens(t *testing.T) {
	ctx := context.Background()
	m, _ := newManager(t)
	nt, err := m.CreateToken(ctx, core.APIToken{Name: "ci", Scopes: []string{core.ScopeIngest}, JobSlugs: []string{"db"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(nt.Secret) != len(TokenPrefix)+32 {
		t.Fatalf("secret length = %d", len(nt.Secret))
	}
	if nt.Token.Prefix != nt.Secret[:len(TokenPrefix)+8] {
		t.Fatalf("prefix = %q", nt.Token.Prefix)
	}
	p, err := m.ResolveToken(ctx, nt.Secret)
	if err != nil {
		t.Fatal(err)
	}
	if !p.HasScope(core.ScopeIngest) || p.HasScope(core.ScopeAdmin) {
		t.Fatalf("scopes = %v", p.Scopes)
	}
	if !p.AllowsJob("db") || p.AllowsJob("other") {
		t.Fatal("job restriction not enforced")
	}
	if _, err := m.ResolveToken(ctx, "bvt_bogus"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}

	past := time.Now().UTC().Add(-time.Hour)
	expired, err := m.CreateToken(ctx, core.APIToken{Name: "old", Scopes: []string{core.ScopeRead}, ExpiresAt: &past})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.ResolveToken(ctx, expired.Secret); !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("expected expired, got %v", err)
	}
}

func TestScopeImplications(t *testing.T) {
	admin := &Principal{Scopes: []string{core.ScopeAdmin}}
	if !admin.HasScope(core.ScopeRead) || !admin.HasScope(core.ScopeRun) || !admin.HasScope(core.ScopeIngest) {
		t.Fatal("admin should imply everything")
	}
	run := &Principal{Scopes: []string{core.ScopeRun}}
	if !run.HasScope(core.ScopeRead) || run.HasScope(core.ScopeAdmin) {
		t.Fatalf("run scope = %v", run.Scopes)
	}
	read := &Principal{Scopes: []string{core.ScopeRead}}
	if read.HasScope(core.ScopeRun) {
		t.Fatal("read must not imply run")
	}
	var nilP *Principal
	if nilP.HasScope(core.ScopeRead) {
		t.Fatal("nil principal has no scopes")
	}
}

func TestMiddleware(t *testing.T) {
	ctx := context.Background()
	m, _ := newManager(t)
	u, err := m.CreateUser(ctx, core.User{Email: "a@b.c", Role: core.RoleViewer}, "supersecret123")
	if err != nil {
		t.Fatal(err)
	}
	secret, _, err := m.CreateSession(ctx, u.ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	mw := &Middleware{Manager: m}
	var seen *Principal
	handler := mw.Resolve(mw.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = FromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous = %d", rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: secret})
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent || seen == nil || seen.User.ID != u.ID {
		t.Fatalf("cookie auth = %d %+v", rec.Code, seen)
	}

	scoped := mw.Resolve(mw.RequireScope(core.ScopeAdmin)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))
	req = httptest.NewRequest(http.MethodGet, "/x", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: secret})
	rec = httptest.NewRecorder()
	scoped.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer admin scope = %d", rec.Code)
	}

	csrf := mw.Resolve(mw.CSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))
	req = httptest.NewRequest(http.MethodPost, "/x", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: secret})
	rec = httptest.NewRecorder()
	csrf.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing csrf header = %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodPost, "/x", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: secret})
	req.Header.Set(CSRFHeader, CSRFValue)
	rec = httptest.NewRecorder()
	csrf.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("with csrf header = %d", rec.Code)
	}
}

func TestRateLimiter(t *testing.T) {
	l := NewRateLimiter(5, time.Minute)
	now := time.Now()
	for i := 0; i < 5; i++ {
		if !l.Allow("ip", now) {
			t.Fatalf("attempt %d blocked", i)
		}
	}
	if l.Allow("ip", now) {
		t.Fatal("sixth attempt should be blocked")
	}
	if !l.Allow("other", now) {
		t.Fatal("other key should be allowed")
	}
	if !l.Allow("ip", now.Add(2*time.Minute)) {
		t.Fatal("window should have expired")
	}
}

func TestBootstrapAdmin(t *testing.T) {
	ctx := context.Background()
	m, _ := newManager(t)
	created, err := m.BootstrapAdmin(ctx, "root@example.com", "supersecret123")
	if err != nil || !created {
		t.Fatalf("bootstrap: %v %v", created, err)
	}
	created, err = m.BootstrapAdmin(ctx, "root@example.com", "supersecret123")
	if err != nil || created {
		t.Fatalf("second bootstrap should be a no-op: %v %v", created, err)
	}
	needs, err := m.NeedsSetup(ctx)
	if err != nil || needs {
		t.Fatalf("needs setup = %v %v", needs, err)
	}
}
