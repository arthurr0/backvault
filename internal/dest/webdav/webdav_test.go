package webdav

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/dest"
)

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type davServer struct {
	root string
	user string
	pass string
}

func (s *davServer) local(r *http.Request) (string, bool) {
	clean := filepath.Clean("/" + strings.TrimPrefix(decode(r.URL.EscapedPath()), "/dav"))
	if strings.Contains(clean, "..") {
		return "", false
	}
	return filepath.Join(s.root, clean), true
}

func decode(p string) string {
	out, err := url.PathUnescape(p)
	if err != nil {
		return p
	}
	return out
}

func (s *davServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.user != "" {
		user, pass, ok := r.BasicAuth()
		if !ok || user != s.user || pass != s.pass {
			w.Header().Set("WWW-Authenticate", `Basic realm="dav"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
	}
	path, ok := s.local(r)
	if !ok {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodPut:
		if _, err := os.Stat(filepath.Dir(path)); err != nil {
			w.WriteHeader(http.StatusConflict)
			return
		}
		f, err := os.Create(path)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		defer f.Close()
		if _, err := io.Copy(f, r.Body); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
	case http.MethodGet:
		f, err := os.Open(path)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		defer f.Close()
		_, _ = io.Copy(w, f)
	case http.MethodDelete:
		if err := os.RemoveAll(path); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case "MKCOL":
		if _, err := os.Stat(path); err == nil {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if _, err := os.Stat(filepath.Dir(path)); err != nil {
			w.WriteHeader(http.StatusConflict)
			return
		}
		if err := os.Mkdir(path, 0o755); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
	case "MOVE":
		destination := r.Header.Get("Destination")
		u, err := url.Parse(destination)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		target := filepath.Join(s.root, filepath.Clean("/"+strings.TrimPrefix(decode(u.EscapedPath()), "/dav")))
		_ = os.Remove(target)
		if err := os.Rename(path, target); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
	case "PROPFIND":
		info, err := os.Stat(path)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var entries []os.FileInfo
		var hrefs []string
		entries = append(entries, info)
		hrefs = append(hrefs, r.URL.EscapedPath())
		if info.IsDir() && r.Header.Get("Depth") != "0" {
			children, _ := os.ReadDir(path)
			sort.Slice(children, func(i, j int) bool { return children[i].Name() < children[j].Name() })
			for _, c := range children {
				ci, err := c.Info()
				if err != nil {
					continue
				}
				entries = append(entries, ci)
				hrefs = append(hrefs, strings.TrimSuffix(r.URL.EscapedPath(), "/")+"/"+url.PathEscape(c.Name()))
			}
		}
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.WriteHeader(http.StatusMultiStatus)
		fmt.Fprint(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:">`)
		for i, e := range entries {
			resourceType := ""
			if e.IsDir() {
				resourceType = "<d:collection/>"
			}
			fmt.Fprintf(w, `<d:response><d:href>%s</d:href><d:propstat><d:status>HTTP/1.1 200 OK</d:status><d:prop>`+
				`<d:resourcetype>%s</d:resourcetype><d:getcontentlength>%d</d:getcontentlength>`+
				`<d:getlastmodified>%s</d:getlastmodified></d:prop></d:propstat></d:response>`,
				hrefs[i], resourceType, e.Size(), e.ModTime().UTC().Format(http.TimeFormat))
		}
		fmt.Fprint(w, `</d:multistatus>`)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func openClient(t *testing.T, extra core.Config) (dest.Client, string) {
	t.Helper()
	root := t.TempDir()
	srv := httptest.NewServer(http.StripPrefix("", &davServer{root: root, user: "alice", pass: "s3cret"}))
	t.Cleanup(srv.Close)
	cfg := core.Config{
		"url":       srv.URL + "/dav",
		"user":      "alice",
		"password":  "s3cret",
		"base_path": "backups/backvault",
	}
	for k, v := range extra {
		cfg[k] = v
	}
	c, err := New().Open(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, root
}

func TestValidate(t *testing.T) {
	d := New()
	if err := d.Validate(core.Config{}); err == nil {
		t.Error("url should be required")
	}
	if err := d.Validate(core.Config{"url": "cloud.example.com"}); err == nil {
		t.Error("a url without a scheme should be rejected")
	}
	if err := d.Validate(core.Config{"url": "ftp://cloud.example.com"}); err == nil {
		t.Error("a non http scheme should be rejected")
	}
	if err := d.Validate(core.Config{"url": "https://cloud.example.com/remote.php/dav/files/alice"}); err != nil {
		t.Errorf("valid config rejected: %v", err)
	}
}

func TestPutGetStatListDelete(t *testing.T) {
	c, root := openClient(t, nil)
	if err := c.Test(t.Context()); err != nil {
		t.Fatalf("test: %v", err)
	}
	payload := strings.Repeat("backvault", 500)
	if err := c.Put(t.Context(), "job/job-20240101-000000.tar", strings.NewReader(payload), int64(len(payload))); err != nil {
		t.Fatalf("put: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "backups", "backvault", "job", "job-20240101-000000.tar")); err != nil {
		t.Fatalf("file was not stored: %v", err)
	}
	rc, err := c.Get(t.Context(), "job/job-20240101-000000.tar")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if string(got) != payload {
		t.Error("round trip mismatch")
	}
	st, err := c.Stat(t.Context(), "job/job-20240101-000000.tar")
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if st.Size != int64(len(payload)) {
		t.Errorf("stat size %d, want %d", st.Size, len(payload))
	}
	if st.Path != "job/job-20240101-000000.tar" {
		t.Errorf("stat path %q", st.Path)
	}
	if st.ModTime.IsZero() || time.Since(st.ModTime) > time.Hour {
		t.Errorf("stat mod time %v", st.ModTime)
	}
	items, err := c.List(t.Context(), "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var files []string
	for _, item := range items {
		if !item.IsDir {
			files = append(files, item.Path)
		}
	}
	if len(files) != 1 || files[0] != "job/job-20240101-000000.tar" {
		t.Errorf("list returned %v", files)
	}
	if err := c.Delete(t.Context(), "job/job-20240101-000000.tar"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := c.Delete(t.Context(), "job/job-20240101-000000.tar"); err != nil {
		t.Errorf("delete of a missing object should return nil, got %v", err)
	}
}

func TestPutLeavesNoPartialFile(t *testing.T) {
	c, root := openClient(t, nil)
	if err := c.Put(t.Context(), "a/b/c.bin", strings.NewReader("data"), 4); err != nil {
		t.Fatalf("put: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "backups", "backvault", "a", "b"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "c.bin" {
		t.Errorf("unexpected directory contents %v", entries)
	}
}

func TestPathTraversalIsRefused(t *testing.T) {
	c, _ := openClient(t, nil)
	if err := c.Put(t.Context(), "../../escape.bin", strings.NewReader("x"), 1); err == nil {
		t.Error("path traversal should be refused")
	}
}

func TestBadCredentialsAreReported(t *testing.T) {
	c, _ := openClient(t, core.Config{"password": "wrong"})
	if err := c.Test(t.Context()); err == nil {
		t.Error("bad credentials should fail the test")
	}
}
