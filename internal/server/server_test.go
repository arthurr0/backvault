package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arthurr0/backvault/internal/auth"
	"github.com/arthurr0/backvault/internal/config"
	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/engine"
	"github.com/arthurr0/backvault/internal/secrets"
	"github.com/arthurr0/backvault/internal/store"
)

type testEnv struct {
	t       *testing.T
	server  *Server
	http    *httptest.Server
	store   *store.Store
	engine  *engine.Engine
	secrets *secrets.Cipher
	cookie  *http.Cookie
	baseURL string
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(dir, "backvault.db"))
	if err != nil {
		t.Fatal(err)
	}
	key, err := secrets.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := secrets.New(key)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	eng := engine.New(engine.Deps{Store: st, Secrets: cipher, WorkDir: filepath.Join(dir, "work"), Logger: log})
	cfg := config.Default()
	cfg.DataDir = dir
	cfg.MetricsToken = ""
	srv, err := New(Options{Store: st, Engine: eng, Auth: auth.NewManager(st), Secrets: cipher, Config: cfg, Logger: log, StaticFS: nil})
	if err != nil {
		t.Fatal(err)
	}
	eng.SetObserver(srv.Metrics())
	if err := eng.Start(ctx); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		ts.Close()
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = eng.Stop(stopCtx)
		_ = st.Close()
	})
	return &testEnv{t: t, server: srv, http: ts, store: st, engine: eng, secrets: cipher, baseURL: ts.URL}
}

func (e *testEnv) request(method, path string, body any) *http.Request {
	e.t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			e.t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, e.baseURL+path, reader)
	if err != nil {
		e.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set(auth.CSRFHeader, auth.CSRFValue)
	if e.cookie != nil {
		req.AddCookie(e.cookie)
	}
	return req
}

func (e *testEnv) do(req *http.Request) *http.Response {
	e.t.Helper()
	resp, err := e.http.Client().Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	return resp
}

func (e *testEnv) call(method, path string, body any, out any) *http.Response {
	e.t.Helper()
	resp := e.do(e.request(method, path, body))
	if out != nil {
		defer resp.Body.Close()
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil && err != io.EOF {
			e.t.Fatalf("%s %s: decode: %v", method, path, err)
		}
	}
	return resp
}

func (e *testEnv) setup() core.User {
	e.t.Helper()
	var out struct {
		User core.User `json:"user"`
	}
	resp := e.call("POST", "/api/v1/setup", map[string]string{
		"name": "Admin", "email": "admin@example.com", "password": "supersecret123",
	}, &out)
	if resp.StatusCode != http.StatusCreated {
		e.t.Fatalf("setup status = %d", resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		if c.Name == auth.CookieName {
			e.cookie = c
		}
	}
	if e.cookie == nil {
		e.t.Fatal("setup did not set a session cookie")
	}
	return out.User
}

func errorBody(t *testing.T, resp *http.Response) apiError {
	t.Helper()
	defer resp.Body.Close()
	var envelope errorEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	return envelope.Error
}

func TestSetupAndLoginFlow(t *testing.T) {
	env := newEnv(t)

	var status struct {
		NeedsSetup bool `json:"needsSetup"`
	}
	resp := env.call("GET", "/api/v1/setup/status", nil, &status)
	if resp.StatusCode != http.StatusOK || !status.NeedsSetup {
		t.Fatalf("setup status = %d %+v", resp.StatusCode, status)
	}

	resp = env.call("GET", "/api/v1/jobs", nil, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous jobs = %d", resp.StatusCode)
	}

	user := env.setup()
	if user.Role != core.RoleAdmin {
		t.Fatalf("role = %s", user.Role)
	}

	resp = env.call("GET", "/api/v1/setup/status", nil, &status)
	resp.Body.Close()
	if status.NeedsSetup {
		t.Fatal("setup should be complete")
	}

	resp = env.call("POST", "/api/v1/setup", map[string]string{"name": "x", "email": "y@z.c", "password": "supersecret123"}, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("second setup = %d", resp.StatusCode)
	}

	var me map[string]any
	resp = env.call("GET", "/api/v1/auth/me", nil, &me)
	if resp.StatusCode != http.StatusOK || me["authType"] != "session" {
		t.Fatalf("me = %d %+v", resp.StatusCode, me)
	}

	fresh := &testEnv{t: t, http: env.http, baseURL: env.baseURL, store: env.store, engine: env.engine, server: env.server}
	resp = fresh.call("POST", "/api/v1/auth/login", map[string]string{"email": "admin@example.com", "password": "wrong"}, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad login = %d", resp.StatusCode)
	}
	resp = fresh.call("POST", "/api/v1/auth/login", map[string]string{"email": "admin@example.com", "password": "supersecret123"}, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login = %d", resp.StatusCode)
	}

	resp = env.call("POST", "/api/v1/auth/logout", nil, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout = %d", resp.StatusCode)
	}
}

func TestCSRFRequiredForCookieMutations(t *testing.T) {
	env := newEnv(t)
	env.setup()
	req := env.request("POST", "/api/v1/sources", map[string]any{"name": "x", "kind": "testsource"})
	req.Header.Del(auth.CSRFHeader)
	resp := env.do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("missing CSRF header = %d", resp.StatusCode)
	}
}

func TestLoginRateLimit(t *testing.T) {
	env := newEnv(t)
	env.setup()
	env.cookie = nil
	blocked := false
	for i := 0; i < 8; i++ {
		resp := env.call("POST", "/api/v1/auth/login", map[string]string{"email": "admin@example.com", "password": "nope"}, nil)
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			blocked = true
			break
		}
	}
	if !blocked {
		t.Fatal("login rate limit did not trigger")
	}
}

func (e *testEnv) createSource(name string) core.Source {
	e.t.Helper()
	var src core.Source
	resp := e.call("POST", "/api/v1/sources", map[string]any{
		"name": name, "kind": "testsource",
		"config": map[string]any{"payload": "hello world", "password": "s3cr3t"},
	}, &src)
	if resp.StatusCode != http.StatusCreated {
		e.t.Fatalf("create source = %d", resp.StatusCode)
	}
	return src
}

func (e *testEnv) createDestination(name, bucket string) core.Destination {
	e.t.Helper()
	var d core.Destination
	resp := e.call("POST", "/api/v1/destinations", map[string]any{
		"name": name, "kind": "testdest",
		"config": map[string]any{"bucket": bucket, "secret_key": "abc"},
	}, &d)
	if resp.StatusCode != http.StatusCreated {
		e.t.Fatalf("create destination = %d", resp.StatusCode)
	}
	return d
}

func TestSourceAndDestinationCRUD(t *testing.T) {
	env := newEnv(t)
	env.setup()

	src := env.createSource("files")
	if src.Config.String("password") != core.SecretMask {
		t.Fatalf("secret leaked: %v", src.Config)
	}
	if src.Config.String("payload") != "hello world" {
		t.Fatalf("config = %v", src.Config)
	}

	stored, err := env.store.Sources.Get(context.Background(), src.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !secrets.IsEncrypted(stored.Config.String("password")) {
		t.Fatalf("secret was not encrypted at rest: %v", stored.Config)
	}

	var updated core.Source
	resp := env.call("PUT", "/api/v1/sources/"+src.ID, map[string]any{
		"name": "files renamed", "kind": "testsource",
		"config": map[string]any{"payload": "changed", "password": core.SecretMask},
	}, &updated)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update source = %d", resp.StatusCode)
	}
	stored, err = env.store.Sources.Get(context.Background(), src.ID)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := env.server.secrets.Decrypt(stored.Config.String("password"))
	if err != nil {
		t.Fatal(err)
	}
	if plain != "s3cr3t" {
		t.Fatalf("masked secret was not preserved: %q", plain)
	}

	resp = env.call("POST", "/api/v1/sources", map[string]any{"name": "bad", "kind": "nope"}, nil)
	body := errorBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest || body.Code != codeValidation {
		t.Fatalf("unknown kind = %d %+v", resp.StatusCode, body)
	}

	resp = env.call("POST", "/api/v1/sources", map[string]any{"name": "missing", "kind": "testsource", "config": map[string]any{}}, nil)
	body = errorBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing required field = %d %+v", resp.StatusCode, body)
	}

	var testResult engine.TestResult
	resp = env.call("POST", "/api/v1/sources/"+src.ID+"/test", nil, &testResult)
	if resp.StatusCode != http.StatusOK || !testResult.OK {
		t.Fatalf("source test = %d %+v", resp.StatusCode, testResult)
	}

	d := env.createDestination("disk", "b1")
	if d.Config.String("secret_key") != core.SecretMask {
		t.Fatalf("destination secret leaked: %v", d.Config)
	}
	resp = env.call("POST", "/api/v1/destinations/"+d.ID+"/test", nil, &testResult)
	if resp.StatusCode != http.StatusOK || !testResult.OK {
		t.Fatalf("destination test = %d %+v", resp.StatusCode, testResult)
	}

	resp = env.call("DELETE", "/api/v1/sources/"+src.ID, nil, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete source = %d", resp.StatusCode)
	}
}

func (e *testEnv) createJob(name string, srcID string, destIDs []string, extra map[string]any) core.Job {
	e.t.Helper()
	body := map[string]any{
		"name": name, "sourceId": srcID, "destinationIds": destIDs,
		"compression": "none", "encryption": "none",
	}
	for k, v := range extra {
		body[k] = v
	}
	var job core.Job
	resp := e.call("POST", "/api/v1/jobs", body, &job)
	if resp.StatusCode != http.StatusCreated {
		e.t.Fatalf("create job = %d", resp.StatusCode)
	}
	return job
}

func TestJobValidationAndCRUD(t *testing.T) {
	env := newEnv(t)
	env.setup()
	src := env.createSource("files")
	d := env.createDestination("disk", "jobs-bucket")

	job := env.createJob("Nightly Database", src.ID, []string{d.ID}, map[string]any{"schedule": "0 2 * * *", "timezone": "Europe/Warsaw"})
	if job.Slug != "nightly-database" {
		t.Fatalf("slug = %q", job.Slug)
	}
	if job.NextRunAt == nil {
		t.Fatal("next run was not computed")
	}
	if job.SourceName != src.Name || len(job.DestinationNames) != 1 {
		t.Fatalf("computed fields missing: %+v", job)
	}

	second := env.createJob("Nightly Database", src.ID, []string{d.ID}, nil)
	if second.Slug != "nightly-database-2" {
		t.Fatalf("collision slug = %q", second.Slug)
	}

	resp := env.call("POST", "/api/v1/jobs", map[string]any{
		"name": "Broken", "sourceId": src.ID, "destinationIds": []string{d.ID}, "schedule": "not a cron",
	}, nil)
	body := errorBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest || body.Fields["schedule"] == "" {
		t.Fatalf("cron validation = %d %+v", resp.StatusCode, body)
	}

	resp = env.call("POST", "/api/v1/jobs", map[string]any{
		"name": "Broken", "sourceId": src.ID, "destinationIds": []string{d.ID}, "timezone": "Mars/Olympus",
	}, nil)
	body = errorBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest || body.Fields["timezone"] == "" {
		t.Fatalf("timezone validation = %+v", body)
	}

	resp = env.call("POST", "/api/v1/jobs", map[string]any{
		"name": "Broken", "sourceId": src.ID, "destinationIds": []string{d.ID}, "encryption": "age",
	}, nil)
	body = errorBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest || body.Fields["encryptionPassphrase"] == "" {
		t.Fatalf("passphrase validation = %+v", body)
	}

	resp = env.call("POST", "/api/v1/jobs", map[string]any{
		"name": "Broken", "sourceId": "missing", "destinationIds": []string{d.ID},
	}, nil)
	body = errorBody(t, resp)
	if body.Fields["sourceId"] == "" {
		t.Fatalf("source validation = %+v", body)
	}

	resp = env.call("POST", "/api/v1/jobs", map[string]any{
		"name": "Broken", "sourceId": src.ID, "destinationIds": []string{},
	}, nil)
	body = errorBody(t, resp)
	if body.Fields["destinationIds"] == "" {
		t.Fatalf("destination validation = %+v", body)
	}

	resp = env.call("POST", "/api/v1/jobs", map[string]any{
		"name": "Broken", "sourceId": src.ID, "destinationIds": []string{d.ID},
		"retention": map[string]any{"keepLast": -1},
	}, nil)
	body = errorBody(t, resp)
	if body.Fields["retention"] == "" {
		t.Fatalf("retention validation = %+v", body)
	}

	var bySlug core.Job
	resp = env.call("GET", "/api/v1/jobs/nightly-database", nil, &bySlug)
	if resp.StatusCode != http.StatusOK || bySlug.ID != job.ID {
		t.Fatalf("get by slug = %d %+v", resp.StatusCode, bySlug)
	}

	var disabled core.Job
	resp = env.call("POST", "/api/v1/jobs/"+job.Slug+"/disable", nil, &disabled)
	if resp.StatusCode != http.StatusOK || disabled.Enabled {
		t.Fatalf("disable = %d %+v", resp.StatusCode, disabled)
	}
	resp = env.call("POST", "/api/v1/jobs/"+job.Slug+"/enable", nil, &disabled)
	if resp.StatusCode != http.StatusOK || !disabled.Enabled {
		t.Fatalf("enable = %d", resp.StatusCode)
	}

	var duplicate core.Job
	resp = env.call("POST", "/api/v1/jobs/"+job.Slug+"/duplicate", nil, &duplicate)
	if resp.StatusCode != http.StatusCreated || duplicate.Slug != job.Slug+"-copy" {
		t.Fatalf("duplicate = %d %+v", resp.StatusCode, duplicate)
	}

	var preview listEnvelope
	resp = env.call("GET", "/api/v1/jobs/"+job.Slug+"/schedule/preview?count=3", nil, &preview)
	if resp.StatusCode != http.StatusOK || preview.Total != 3 {
		t.Fatalf("schedule preview = %d %+v", resp.StatusCode, preview)
	}

	resp = env.call("DELETE", "/api/v1/destinations/"+d.ID, nil, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("destination in use = %d", resp.StatusCode)
	}
}

func TestRunJobDownloadAndAudit(t *testing.T) {
	env := newEnv(t)
	env.setup()
	src := env.createSource("files")
	d := env.createDestination("disk", "run-bucket")
	job := env.createJob("Payload Job", src.ID, []string{d.ID}, nil)

	var queued struct {
		Run core.Run `json:"run"`
	}
	resp := env.call("POST", "/api/v1/jobs/"+job.Slug+"/run", map[string]any{}, &queued)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("run = %d", resp.StatusCode)
	}

	run := waitRun(t, env, queued.Run.ID)
	if run.Status != core.RunSuccess {
		t.Fatalf("run status = %s error = %s", run.Status, run.Error)
	}

	var artifacts struct {
		Items []core.Artifact `json:"items"`
		Total int             `json:"total"`
	}
	resp = env.call("GET", "/api/v1/artifacts?job="+job.Slug, nil, &artifacts)
	if resp.StatusCode != http.StatusOK || artifacts.Total != 1 {
		t.Fatalf("artifacts = %d %+v", resp.StatusCode, artifacts)
	}

	resp = env.do(env.request("GET", "/api/v1/artifacts/"+artifacts.Items[0].ID+"/download", nil))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("download = %d", resp.StatusCode)
	}
	if !strings.Contains(resp.Header.Get("Content-Disposition"), run.Filename) {
		t.Fatalf("content disposition = %q", resp.Header.Get("Content-Disposition"))
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello world" {
		t.Fatalf("downloaded %q", data)
	}

	logResp := env.do(env.request("GET", "/api/v1/runs/"+run.ID+"/log", nil))
	defer logResp.Body.Close()
	logText, _ := io.ReadAll(logResp.Body)
	if !strings.Contains(string(logText), "test dump") {
		t.Fatalf("run log = %q", logText)
	}

	var audit struct {
		Items []core.AuditEntry `json:"items"`
		Total int               `json:"total"`
	}
	resp = env.call("GET", "/api/v1/audit?action=job.run", nil, &audit)
	if resp.StatusCode != http.StatusOK || audit.Total != 1 {
		t.Fatalf("audit = %d %+v", resp.StatusCode, audit)
	}

	var dashboard core.DashboardStats
	resp = env.call("GET", "/api/v1/dashboard", nil, &dashboard)
	if resp.StatusCode != http.StatusOK || dashboard.Jobs != 1 || dashboard.Artifacts != 1 {
		t.Fatalf("dashboard = %d %+v", resp.StatusCode, dashboard)
	}
}

func waitRun(t *testing.T, env *testEnv, runID string) core.Run {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		var run core.Run
		resp := env.call("GET", "/api/v1/runs/"+runID, nil, &run)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("get run = %d", resp.StatusCode)
		}
		if run.Status.Terminal() {
			return run
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("run did not finish")
	return core.Run{}
}

func TestTokenAuthAndIngest(t *testing.T) {
	env := newEnv(t)
	env.setup()
	src := env.createSource("push source")
	d := env.createDestination("disk", "ingest-bucket")
	job := env.createJob("Pushed Job", src.ID, []string{d.ID}, nil)

	var created struct {
		Token  core.APIToken `json:"token"`
		Secret string        `json:"secret"`
	}
	resp := env.call("POST", "/api/v1/tokens", map[string]any{
		"name": "ci", "scopes": []string{core.ScopeIngest, core.ScopeRead}, "jobSlugs": []string{job.Slug},
	}, &created)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create token = %d", resp.StatusCode)
	}
	if !strings.HasPrefix(created.Secret, "bvt_") || len(created.Secret) != 36 {
		t.Fatalf("secret = %q", created.Secret)
	}

	payload := "pushed content"
	req, err := http.NewRequest("POST", env.baseURL+"/api/v1/ingest/"+job.Slug, strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+created.Secret)
	req.Header.Set("X-Backvault-Filename", "dump.sql")
	resp = env.do(req)
	var ingested struct {
		Run       core.Run        `json:"run"`
		Artifacts []core.Artifact `json:"artifacts"`
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&ingested); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("ingest = %d %+v", resp.StatusCode, ingested.Run)
	}
	if len(ingested.Artifacts) != 1 || !strings.HasSuffix(ingested.Artifacts[0].Filename, ".sql") {
		t.Fatalf("artifacts = %+v", ingested.Artifacts)
	}

	req, _ = http.NewRequest("POST", env.baseURL+"/api/v1/ingest/"+job.Slug, strings.NewReader("x"))
	req.Header.Set("Authorization", "Bearer bvt_wrongwrongwrongwrongwrongwrongwr")
	resp = env.do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad token = %d", resp.StatusCode)
	}

	other := env.createJob("Other Job", src.ID, []string{d.ID}, nil)
	req, _ = http.NewRequest("POST", env.baseURL+"/api/v1/ingest/"+other.Slug, strings.NewReader("x"))
	req.Header.Set("Authorization", "Bearer "+created.Secret)
	resp = env.do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("job restriction = %d", resp.StatusCode)
	}

	req, _ = http.NewRequest("POST", env.baseURL+"/api/v1/jobs/"+job.Slug+"/run", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer "+created.Secret)
	resp = env.do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("ingest token should not run jobs, got %d", resp.StatusCode)
	}
}

func TestViewerCannotMutate(t *testing.T) {
	env := newEnv(t)
	env.setup()
	var viewer core.User
	resp := env.call("POST", "/api/v1/users", map[string]any{
		"email": "viewer@example.com", "name": "Viewer", "role": "viewer", "password": "supersecret123",
	}, &viewer)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create user = %d", resp.StatusCode)
	}

	viewerEnv := &testEnv{t: t, http: env.http, baseURL: env.baseURL, store: env.store, engine: env.engine, server: env.server}
	resp = viewerEnv.call("POST", "/api/v1/auth/login", map[string]string{"email": "viewer@example.com", "password": "supersecret123"}, nil)
	resp.Body.Close()
	for _, c := range resp.Cookies() {
		if c.Name == auth.CookieName {
			viewerEnv.cookie = c
		}
	}
	if viewerEnv.cookie == nil {
		t.Fatal("no session cookie for the viewer")
	}

	resp = viewerEnv.call("GET", "/api/v1/jobs", nil, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("viewer list jobs = %d", resp.StatusCode)
	}
	resp = viewerEnv.call("POST", "/api/v1/sources", map[string]any{"name": "x", "kind": "testsource"}, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer create source = %d", resp.StatusCode)
	}

	resp = env.call("DELETE", "/api/v1/users/"+viewer.ID, nil, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete viewer = %d", resp.StatusCode)
	}
	admins, err := env.store.Users.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	resp = env.call("DELETE", "/api/v1/users/"+admins[0].ID, nil, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("deleting the last admin = %d", resp.StatusCode)
	}
}

func TestSettingsAndMeta(t *testing.T) {
	env := newEnv(t)
	env.setup()

	var settings core.Settings
	resp := env.call("GET", "/api/v1/settings", nil, &settings)
	if resp.StatusCode != http.StatusOK || settings.MaxConcurrentRuns != 2 {
		t.Fatalf("settings = %d %+v", resp.StatusCode, settings)
	}
	settings.SiteName = "Backups"
	settings.MaxConcurrentRuns = 4
	var saved core.Settings
	resp = env.call("PUT", "/api/v1/settings", settings, &saved)
	if resp.StatusCode != http.StatusOK || saved.MaxConcurrentRuns != 4 {
		t.Fatalf("update settings = %d %+v", resp.StatusCode, saved)
	}
	bad := settings
	bad.MaxConcurrentRuns = 0
	resp = env.call("PUT", "/api/v1/settings", bad, nil)
	body := errorBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest || body.Fields["maxConcurrentRuns"] == "" {
		t.Fatalf("settings validation = %+v", body)
	}

	for _, path := range []string{"/api/v1/meta/version", "/api/v1/meta/sources", "/api/v1/meta/destinations", "/api/v1/meta/notifiers", "/api/v1/meta/tools", "/api/v1/meta/timezones"} {
		resp := env.call("GET", path, nil, nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s = %d", path, resp.StatusCode)
		}
	}

	resp = env.do(env.request("GET", "/healthz", nil))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz = %d", resp.StatusCode)
	}
	resp = env.do(env.request("GET", "/readyz", nil))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("readyz = %d", resp.StatusCode)
	}
	resp = env.do(env.request("GET", "/metrics", nil))
	defer resp.Body.Close()
	metricsBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(metricsBody), "backvault_jobs") {
		t.Fatalf("metrics = %d", resp.StatusCode)
	}
}

func TestExportImport(t *testing.T) {
	env := newEnv(t)
	env.setup()
	src := env.createSource("files")
	d := env.createDestination("disk", "export-bucket")
	env.createJob("Exported Job", src.ID, []string{d.ID}, map[string]any{"schedule": "@daily"})

	resp := env.do(env.request("GET", "/api/v1/export", nil))
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export = %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), "exported-job") {
		t.Fatalf("export body = %s", body)
	}
	if strings.Contains(string(body), "s3cr3t") {
		t.Fatal("export leaked a secret")
	}
	if !strings.Contains(string(body), core.SecretMask) {
		t.Fatalf("export should mask secrets: %s", body)
	}

	req, err := http.NewRequest("POST", env.baseURL+"/api/v1/import?dryRun=1", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(auth.CSRFHeader, auth.CSRFValue)
	req.AddCookie(env.cookie)
	resp = env.do(req)
	defer resp.Body.Close()
	var plan struct {
		DryRun  bool `json:"dryRun"`
		Total   int  `json:"total"`
		Changes []struct {
			Kind   string `json:"kind"`
			Action string `json:"action"`
		} `json:"changes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&plan); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || !plan.DryRun || plan.Total != 3 {
		t.Fatalf("dry run = %d %+v", resp.StatusCode, plan)
	}
	for _, change := range plan.Changes {
		if change.Action != "update" {
			t.Fatalf("expected updates, got %+v", change)
		}
	}

	secretResp := env.do(env.request("GET", "/api/v1/export?includeSecrets=1", nil))
	defer secretResp.Body.Close()
	secretBody, _ := io.ReadAll(secretResp.Body)
	if !strings.Contains(string(secretBody), "s3cr3t") {
		t.Fatal("includeSecrets did not include the secret")
	}
	var audit struct {
		Total int `json:"total"`
	}
	r2 := env.call("GET", "/api/v1/audit?action=export.secrets", nil, &audit)
	r2.Body.Close()
	if audit.Total != 1 {
		t.Fatalf("export.secrets audit = %d", audit.Total)
	}
}

func TestEventStreamAndRunLogStream(t *testing.T) {
	env := newEnv(t)
	env.setup()
	src := env.createSource("files")
	d := env.createDestination("disk", "sse-bucket")
	job := env.createJob("SSE Job", src.ID, []string{d.ID}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", env.baseURL+"/api/v1/events/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(env.cookie)
	resp, err := env.http.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("event stream = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content type = %q", ct)
	}

	var queued struct {
		Run core.Run `json:"run"`
	}
	runResp := env.call("POST", "/api/v1/jobs/"+job.Slug+"/run", map[string]any{}, &queued)
	runResp.Body.Close()

	found := make(chan bool, 1)
	go func() {
		buf := make([]byte, 4096)
		seen := ""
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				seen += string(buf[:n])
				if strings.Contains(seen, "event: run.updated") {
					found <- true
					return
				}
			}
			if err != nil {
				found <- false
				return
			}
		}
	}()
	select {
	case ok := <-found:
		if !ok {
			t.Fatal("event stream closed before a run event arrived")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no run.updated event")
	}

	run := waitRun(t, env, queued.Run.ID)
	logReq, err := http.NewRequestWithContext(ctx, "GET", env.baseURL+"/api/v1/runs/"+run.ID+"/log/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	logReq.AddCookie(env.cookie)
	logResp, err := env.http.Client().Do(logReq)
	if err != nil {
		t.Fatal(err)
	}
	defer logResp.Body.Close()
	streamed, err := io.ReadAll(logResp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(streamed), "event: line") || !strings.Contains(string(streamed), "event: done") {
		t.Fatalf("log stream = %q", streamed)
	}
}

func TestPaginationAndErrorEnvelope(t *testing.T) {
	env := newEnv(t)
	env.setup()
	src := env.createSource("files")
	d := env.createDestination("disk", "page-bucket")
	for i := 0; i < 3; i++ {
		env.createJob("Job "+string(rune('A'+i)), src.ID, []string{d.ID}, nil)
	}
	var page struct {
		Items []core.Job `json:"items"`
		Total int        `json:"total"`
	}
	resp := env.call("GET", "/api/v1/jobs?limit=2&offset=1", nil, &page)
	if resp.StatusCode != http.StatusOK || page.Total != 3 || len(page.Items) != 2 {
		t.Fatalf("pagination = %d %+v", resp.StatusCode, page)
	}
	resp = env.call("GET", "/api/v1/jobs?limit=abc", nil, nil)
	body := errorBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest || body.Fields["limit"] == "" {
		t.Fatalf("limit validation = %+v", body)
	}
	resp = env.call("GET", "/api/v1/jobs/missing", nil, nil)
	body = errorBody(t, resp)
	if resp.StatusCode != http.StatusNotFound || body.Code != codeNotFound {
		t.Fatalf("not found = %d %+v", resp.StatusCode, body)
	}
	resp = env.call("GET", "/api/v1/nope", nil, nil)
	body = errorBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown endpoint = %d", resp.StatusCode)
	}
}

func TestChannelsCRUD(t *testing.T) {
	env := newEnv(t)
	env.setup()
	var channel core.NotificationChannel
	resp := env.call("POST", "/api/v1/notifications/channels", map[string]any{
		"name": "ops", "kind": "testnotify", "enabled": true,
		"events": []string{core.EventRunFailed},
		"config": map[string]any{"token": "abc"},
	}, &channel)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create channel = %d", resp.StatusCode)
	}
	if channel.Config.String("token") != core.SecretMask {
		t.Fatalf("channel secret leaked: %v", channel.Config)
	}
	resp = env.call("POST", "/api/v1/notifications/channels", map[string]any{
		"name": "bad", "kind": "testnotify", "events": []string{"nope"},
	}, nil)
	body := errorBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest || body.Fields["events"] == "" {
		t.Fatalf("event validation = %+v", body)
	}
	var result engine.TestResult
	resp = env.call("POST", "/api/v1/notifications/channels/"+channel.ID+"/test", nil, &result)
	if resp.StatusCode != http.StatusOK || !result.OK {
		t.Fatalf("channel test = %d %+v", resp.StatusCode, result)
	}
	resp = env.call("DELETE", "/api/v1/notifications/channels/"+channel.ID, nil, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete channel = %d", resp.StatusCode)
	}
}

func TestPasswordChange(t *testing.T) {
	env := newEnv(t)
	user := env.setup()
	resp := env.call("PUT", "/api/v1/users/"+user.ID+"/password", map[string]any{
		"currentPassword": "wrong", "newPassword": "anothersecret123",
	}, nil)
	body := errorBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest || body.Fields["currentPassword"] == "" {
		t.Fatalf("current password check = %+v", body)
	}
	resp = env.call("PUT", "/api/v1/users/"+user.ID+"/password", map[string]any{
		"currentPassword": "supersecret123", "newPassword": "anothersecret123",
	}, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("password change = %d", resp.StatusCode)
	}
	resp = env.call("GET", "/api/v1/jobs", nil, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("sessions should be invalidated, got %d", resp.StatusCode)
	}
}

func TestSlugHelpers(t *testing.T) {
	cases := map[string]string{
		"Nightly Database": "nightly-database",
		"  Weird__Name.. ": "weird-name",
		"MiXeD/Case":       "mixed-case",
		"---":              "",
		"Ärger 2024":       "rger-2024",
	}
	for input, want := range cases {
		if got := slugify(input); got != want {
			t.Fatalf("slugify(%q) = %q, want %q", input, got, want)
		}
	}
	if !validSlug("nightly-db-2") || validSlug("Bad Slug") || validSlug("-leading") {
		t.Fatal("slug validation is wrong")
	}
}
