package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/arthurr0/backvault/internal/auth"
	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/remote"
	"github.com/arthurr0/backvault/internal/store"
)

func readExport(t *testing.T, resp *http.Response) exportDocument {
	t.Helper()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var doc exportDocument
	if err := yaml.Unmarshal(body, &doc); err != nil {
		t.Fatalf("decode export: %v", err)
	}
	return doc
}

func (e *testEnv) importYAML(body string, dryRun bool, out any) *http.Response {
	e.t.Helper()
	path := e.baseURL + "/api/v1/import"
	if dryRun {
		path += "?dryRun=1"
	}
	req, err := http.NewRequest("POST", path, strings.NewReader(body))
	if err != nil {
		e.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/yaml")
	req.Header.Set(auth.CSRFHeader, auth.CSRFValue)
	if e.cookie != nil {
		req.AddCookie(e.cookie)
	}
	resp := e.do(req)
	if out != nil {
		defer resp.Body.Close()
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil && err != io.EOF {
			e.t.Fatalf("decode import response: %v", err)
		}
	}
	return resp
}

func storeHostFilter() store.HostFilter {
	return store.HostFilter{Page: store.Page{Limit: store.MaxLimit}}
}

func (e *testEnv) generateKey() keygenResponse {
	e.t.Helper()
	var out keygenResponse
	resp := e.call("POST", "/api/v1/hosts/keygen", map[string]any{}, &out)
	if resp.StatusCode != http.StatusOK {
		e.t.Fatalf("keygen = %d", resp.StatusCode)
	}
	return out
}

func (e *testEnv) createHost(name string, extra map[string]any) core.Host {
	e.t.Helper()
	key := e.generateKey()
	body := map[string]any{
		"name": name, "address": "10.0.0.7", "port": 22, "user": "backup",
		"auth": "key", "privateKey": key.PrivateKey, "connectTimeoutSeconds": 1,
	}
	for k, v := range extra {
		body[k] = v
	}
	var host core.Host
	resp := e.call("POST", "/api/v1/hosts", body, &host)
	if resp.StatusCode != http.StatusCreated {
		e.t.Fatalf("create host = %d", resp.StatusCode)
	}
	return host
}

func (e *testEnv) storedHost(id string) core.Host {
	e.t.Helper()
	stored, err := e.store.Hosts.Get(context.Background(), id)
	if err != nil {
		e.t.Fatal(err)
	}
	plain, err := e.secrets.DecryptHost(stored)
	if err != nil {
		e.t.Fatal(err)
	}
	return plain
}

func TestHostCRUDAndMasking(t *testing.T) {
	env := newEnv(t)
	env.setup()

	host := env.createHost("db-01", map[string]any{"description": "primary", "tags": []string{"prod"}, "sudo": true})
	if host.PrivateKey != core.SecretMask {
		t.Fatalf("private key was not masked: %q", host.PrivateKey)
	}
	if !strings.HasPrefix(host.PublicKey, "ssh-ed25519 ") {
		t.Fatalf("public key = %q", host.PublicKey)
	}
	if host.Port != 22 || host.User != "backup" || !host.Sudo {
		t.Fatalf("unexpected host %+v", host)
	}

	stored := env.storedHost(host.ID)
	derived, err := remote.PublicKeyOf(stored)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(host.PublicKey, derived) {
		t.Fatalf("public key %q does not match the stored private key %q", host.PublicKey, derived)
	}

	var list listEnvelope
	resp := env.call("GET", "/api/v1/hosts", nil, &list)
	if resp.StatusCode != http.StatusOK || list.Total != 1 {
		t.Fatalf("list hosts = %d total=%d", resp.StatusCode, list.Total)
	}

	var fetched core.Host
	resp = env.call("GET", "/api/v1/hosts/"+host.ID, nil, &fetched)
	if resp.StatusCode != http.StatusOK || fetched.PrivateKey != core.SecretMask {
		t.Fatalf("get host = %d %+v", resp.StatusCode, fetched)
	}

	var updated core.Host
	resp = env.call("PUT", "/api/v1/hosts/"+host.ID, map[string]any{
		"name": "db-01", "address": "10.0.0.8", "port": 2222, "user": "root",
		"auth": "key", "privateKey": core.SecretMask, "hostKey": "SHA256:abcdef",
	}, &updated)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update host = %d", resp.StatusCode)
	}
	if updated.Address != "10.0.0.8" || updated.Port != 2222 || updated.User != "root" || updated.HostKey != "SHA256:abcdef" {
		t.Fatalf("update not applied: %+v", updated)
	}
	if updated.PrivateKey != core.SecretMask {
		t.Fatalf("private key leaked: %q", updated.PrivateKey)
	}
	if env.storedHost(host.ID).PrivateKey != stored.PrivateKey {
		t.Fatal("the masked private key was not kept on update")
	}

	resp = env.call("PUT", "/api/v1/hosts/"+host.ID, map[string]any{
		"name": "", "address": "", "user": "", "port": 70000, "auth": "magic", "hostKey": "nope",
	}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid update = %d", resp.StatusCode)
	}
	body := errorBody(t, resp)
	if body.Code != codeValidation {
		t.Fatalf("code = %s", body.Code)
	}
	for _, field := range []string{"name", "address", "user", "port", "auth", "hostKey"} {
		if _, ok := body.Fields[field]; !ok {
			t.Fatalf("missing field error for %s: %+v", field, body.Fields)
		}
	}

	resp = env.call("DELETE", "/api/v1/hosts/"+host.ID, nil, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete host = %d", resp.StatusCode)
	}
	resp = env.call("GET", "/api/v1/hosts/"+host.ID, nil, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("get deleted host = %d", resp.StatusCode)
	}
}

func TestHostPasswordAuth(t *testing.T) {
	env := newEnv(t)
	env.setup()

	var host core.Host
	resp := env.call("POST", "/api/v1/hosts", map[string]any{
		"name": "pw", "address": "10.0.0.9", "user": "root", "auth": "password",
	}, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("password host without a password = %d", resp.StatusCode)
	}

	resp = env.call("POST", "/api/v1/hosts", map[string]any{
		"name": "pw", "address": "10.0.0.9", "user": "root", "auth": "password", "password": "hunter2",
	}, &host)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create = %d", resp.StatusCode)
	}
	if host.Password != core.SecretMask || host.PublicKey != "" {
		t.Fatalf("unexpected host %+v", host)
	}
	if env.storedHost(host.ID).Password != "hunter2" {
		t.Fatal("password was not stored")
	}
	if raw, err := env.store.Hosts.Get(context.Background(), host.ID); err != nil || !strings.HasPrefix(raw.Password, "enc:v1:") {
		t.Fatalf("password is not encrypted at rest: %v %q", err, raw.Password)
	}
}

func TestHostKeygenReplacesTheKey(t *testing.T) {
	env := newEnv(t)
	env.setup()

	unsaved := env.generateKey()
	if !strings.HasPrefix(unsaved.PrivateKey, "-----BEGIN OPENSSH PRIVATE KEY-----") {
		t.Fatalf("private key = %q", unsaved.PrivateKey)
	}
	if !strings.HasSuffix(unsaved.PublicKey, "backvault@backvault") {
		t.Fatalf("public key comment = %q", unsaved.PublicKey)
	}
	derived, err := remote.PublicKeyOf(core.Host{PrivateKey: unsaved.PrivateKey})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(unsaved.PublicKey, derived) {
		t.Fatalf("keygen public key %q does not match its private key", unsaved.PublicKey)
	}

	host := env.createHost("keygen-host", nil)
	before := env.storedHost(host.ID).PrivateKey

	var rotated core.Host
	resp := env.call("POST", "/api/v1/hosts/"+host.ID+"/keygen", map[string]any{}, &rotated)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("keygen = %d", resp.StatusCode)
	}
	if rotated.PrivateKey != core.SecretMask {
		t.Fatalf("private key leaked: %q", rotated.PrivateKey)
	}
	if rotated.Auth != core.HostAuthKey {
		t.Fatalf("auth = %s", rotated.Auth)
	}
	after := env.storedHost(host.ID)
	if after.PrivateKey == before {
		t.Fatal("the private key was not replaced")
	}
	rotatedKey, err := remote.PublicKeyOf(after)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rotated.PublicKey, rotatedKey) {
		t.Fatalf("returned public key %q does not match the stored private key", rotated.PublicKey)
	}
}

func TestHostDeleteConflictAndSourceRoundTrip(t *testing.T) {
	env := newEnv(t)
	env.setup()

	host := env.createHost("web-01", nil)

	var src core.Source
	resp := env.call("POST", "/api/v1/sources", map[string]any{
		"name": "remote files", "kind": "testremote", "hostId": host.ID,
		"config": map[string]any{"command": "tar -cf - /etc"},
	}, &src)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create source = %d", resp.StatusCode)
	}
	if src.HostID != host.ID || src.HostName != "web-01" {
		t.Fatalf("source host = %+v", src)
	}

	var fetched core.Source
	resp = env.call("GET", "/api/v1/sources/"+src.ID, nil, &fetched)
	if resp.StatusCode != http.StatusOK || fetched.HostName != "web-01" {
		t.Fatalf("get source = %d %+v", resp.StatusCode, fetched)
	}

	var filtered struct {
		Items []core.Source `json:"items"`
		Total int           `json:"total"`
	}
	resp = env.call("GET", "/api/v1/sources?host="+host.ID, nil, &filtered)
	if resp.StatusCode != http.StatusOK || filtered.Total != 1 || filtered.Items[0].ID != src.ID {
		t.Fatalf("host filter = %d %+v", resp.StatusCode, filtered)
	}

	var withHost core.Host
	resp = env.call("GET", "/api/v1/hosts/"+host.ID, nil, &withHost)
	if resp.StatusCode != http.StatusOK || withHost.SourceCount != 1 {
		t.Fatalf("source count = %d (%d)", withHost.SourceCount, resp.StatusCode)
	}

	resp = env.call("DELETE", "/api/v1/hosts/"+host.ID, nil, nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("delete used host = %d", resp.StatusCode)
	}
	if body := errorBody(t, resp); body.Code != codeConflict || body.Message != "host is used by sources" {
		t.Fatalf("conflict body = %+v", body)
	}

	var detached core.Source
	resp = env.call("PUT", "/api/v1/sources/"+src.ID, map[string]any{
		"name": "remote files", "kind": "testremote", "hostId": "",
		"config": map[string]any{"command": "tar -cf - /etc"},
	}, &detached)
	if resp.StatusCode != http.StatusOK || detached.HostID != "" || detached.HostName != "" {
		t.Fatalf("detach = %d %+v", resp.StatusCode, detached)
	}
	resp = env.call("DELETE", "/api/v1/hosts/"+host.ID, nil, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete host = %d", resp.StatusCode)
	}
}

func TestSourceHostRejectedForLocalDriver(t *testing.T) {
	env := newEnv(t)
	env.setup()
	host := env.createHost("local-only", nil)

	resp := env.call("POST", "/api/v1/sources", map[string]any{
		"name": "plain", "kind": "testsource", "hostId": host.ID,
		"config": map[string]any{"payload": "hello"},
	}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body := errorBody(t, resp)
	if body.Code != codeValidation || body.Fields["hostId"] == "" {
		t.Fatalf("body = %+v", body)
	}

	resp = env.call("POST", "/api/v1/sources", map[string]any{
		"name": "remote", "kind": "testremote", "hostId": "does-not-exist",
		"config": map[string]any{"command": "true"},
	}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown host status = %d", resp.StatusCode)
	}
	if body := errorBody(t, resp); body.Fields["hostId"] != "host does not exist" {
		t.Fatalf("body = %+v", body)
	}
}

func TestHostTestEndpoints(t *testing.T) {
	env := newEnv(t)
	env.setup()

	key := env.generateKey()
	var result struct {
		OK         bool     `json:"ok"`
		Message    string   `json:"message"`
		OS         string   `json:"os"`
		Tools      []string `json:"tools"`
		DurationMS int64    `json:"durationMs"`
	}
	resp := env.call("POST", "/api/v1/hosts/test", map[string]any{
		"address": "192.0.2.1", "port": 22, "user": "root", "auth": "key",
		"privateKey": key.PrivateKey, "connectTimeoutSeconds": 1,
	}, &result)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("test = %d", resp.StatusCode)
	}
	if result.OK || result.Message == "" {
		t.Fatalf("expected a failed test, got %+v", result)
	}
	if strings.Contains(result.Message, "PRIVATE KEY") {
		t.Fatalf("message leaks the key: %q", result.Message)
	}

	host := env.createHost("unreachable", map[string]any{"address": "192.0.2.1"})
	resp = env.call("POST", "/api/v1/hosts/"+host.ID+"/test", map[string]any{}, &result)
	if resp.StatusCode != http.StatusOK || result.OK {
		t.Fatalf("stored host test = %d %+v", resp.StatusCode, result)
	}
	var stored core.Host
	resp = env.call("GET", "/api/v1/hosts/"+host.ID, nil, &stored)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get = %d", resp.StatusCode)
	}
	if stored.LastTestAt == nil || stored.LastTestOK == nil || *stored.LastTestOK {
		t.Fatalf("test result not stored: %+v", stored)
	}
	if stored.LastTestError == "" {
		t.Fatal("expected the test error to be stored")
	}
}

func TestHostExportImport(t *testing.T) {
	env := newEnv(t)
	env.setup()

	host := env.createHost("export-host", map[string]any{"description": "boxed", "tags": []string{"prod"}, "sudo": true})
	var src core.Source
	resp := env.call("POST", "/api/v1/sources", map[string]any{
		"name": "remote files", "kind": "testremote", "hostId": host.ID,
		"config": map[string]any{"command": "tar -cf - /etc"},
	}, &src)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create source = %d", resp.StatusCode)
	}

	resp = env.do(env.request("GET", "/api/v1/export", nil))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export = %d", resp.StatusCode)
	}
	masked := readExport(t, resp)
	if len(masked.Hosts) != 1 {
		t.Fatalf("hosts in export = %d", len(masked.Hosts))
	}
	if masked.Hosts[0].PrivateKey != core.SecretMask {
		t.Fatalf("export leaked the private key: %q", masked.Hosts[0].PrivateKey)
	}
	if masked.Hosts[0].Address != "10.0.0.7" || !masked.Hosts[0].Sudo || masked.Hosts[0].ConnectTimeoutSeconds != 1 {
		t.Fatalf("host fields missing: %+v", masked.Hosts[0])
	}
	if masked.Sources[0].Host != "export-host" {
		t.Fatalf("source host reference = %q", masked.Sources[0].Host)
	}

	resp = env.do(env.request("GET", "/api/v1/export?includeSecrets=1", nil))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export with secrets = %d", resp.StatusCode)
	}
	full := readExport(t, resp)
	if !strings.HasPrefix(full.Hosts[0].PrivateKey, "-----BEGIN OPENSSH PRIVATE KEY-----") {
		t.Fatalf("secret export = %q", full.Hosts[0].PrivateKey)
	}

	full.Hosts[0].Name = "imported-host"
	full.Hosts[0].Address = "10.9.9.9"
	full.Sources[0].Name = "imported source"
	full.Sources[0].Host = "imported-host"
	body, err := yaml.Marshal(full)
	if err != nil {
		t.Fatal(err)
	}

	var plan struct {
		DryRun  bool           `json:"dryRun"`
		Changes []importChange `json:"changes"`
	}
	resp = env.importYAML(string(body), true, &plan)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dry run = %d", resp.StatusCode)
	}
	found := false
	for _, change := range plan.Changes {
		if change.Kind == "host" && change.Name == "imported-host" && change.Action == "create" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no host entry in the plan: %+v", plan.Changes)
	}
	if n, _, err := env.store.Hosts.List(context.Background(), storeHostFilter()); err != nil || len(n) != 1 {
		t.Fatalf("dry run created hosts: %v %d", err, len(n))
	}

	resp = env.importYAML(string(body), false, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("import = %d", resp.StatusCode)
	}
	imported, err := env.store.Hosts.GetByName(context.Background(), "imported-host")
	if err != nil {
		t.Fatal(err)
	}
	if imported.Address != "10.9.9.9" || imported.SourceCount != 1 {
		t.Fatalf("imported host = %+v", imported)
	}
	plain, err := env.secrets.DecryptHost(imported)
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.Validate(plain); err != nil {
		t.Fatalf("imported key does not validate: %v", err)
	}
}

func TestSourceTestAcceptsHostID(t *testing.T) {
	env := newEnv(t)
	env.setup()
	host := env.createHost("test-host", nil)

	var result engineTestResult
	resp := env.call("POST", "/api/v1/sources/test", map[string]any{
		"kind": "testremote", "hostId": host.ID,
		"config": map[string]any{"command": "true", "require_host": true},
	}, &result)
	if resp.StatusCode != http.StatusOK || !result.OK {
		t.Fatalf("test with host = %d %+v", resp.StatusCode, result)
	}

	resp = env.call("POST", "/api/v1/sources/test", map[string]any{
		"kind":   "testremote",
		"config": map[string]any{"command": "true", "require_host": true},
	}, &result)
	if resp.StatusCode != http.StatusOK || result.OK {
		t.Fatalf("test without host = %d %+v", resp.StatusCode, result)
	}

	resp = env.call("POST", "/api/v1/sources/test", map[string]any{
		"kind": "testsource", "hostId": host.ID,
		"config": map[string]any{"payload": "x"},
	}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("local driver with a host = %d", resp.StatusCode)
	}

	var src core.Source
	resp = env.call("POST", "/api/v1/sources", map[string]any{
		"name": "remote cmd", "kind": "testremote",
		"config": map[string]any{"command": "true", "require_host": true},
	}, &src)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create source = %d", resp.StatusCode)
	}
	resp = env.call("POST", "/api/v1/sources/"+src.ID+"/test", map[string]any{"hostId": host.ID}, &result)
	if resp.StatusCode != http.StatusOK || !result.OK {
		t.Fatalf("stored source test with host = %d %+v", resp.StatusCode, result)
	}
	resp = env.call("POST", "/api/v1/sources/"+src.ID+"/test", map[string]any{}, &result)
	if resp.StatusCode != http.StatusOK || result.OK {
		t.Fatalf("stored source test without host = %d %+v", resp.StatusCode, result)
	}
}

type engineTestResult struct {
	OK         bool   `json:"ok"`
	Message    string `json:"message"`
	DurationMS int64  `json:"durationMs"`
}
