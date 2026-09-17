package files

import (
	"archive/tar"
	"bytes"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/source"
)

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func tree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mk := func(rel, content string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("site/index.html", "<h1>hello</h1>")
	mk("site/app.log", "noise")
	mk("site/assets/style.css", "body{}")
	mk("site/node_modules/left-pad/index.js", "module.exports=1")
	mk("site/.env", "SECRET=1")
	return root
}

func archive(t *testing.T, cfg core.Config) (map[string][]byte, error) {
	t.Helper()
	st, err := New().Backup(t.Context(), cfg, testLogger())
	if err != nil {
		return nil, err
	}
	if st.Extension != "tar" {
		t.Errorf("extension %q, want tar", st.Extension)
	}
	entries := map[string][]byte{}
	tr := tar.NewReader(st.Reader)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			st.Reader.Close()
			return nil, err
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			st.Reader.Close()
			return nil, err
		}
		entries[hdr.Name] = data
	}
	if err := st.Reader.Close(); err != nil {
		return entries, err
	}
	return entries, nil
}

func names(entries map[string][]byte) []string {
	out := make([]string, 0, len(entries))
	for k := range entries {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestValidate(t *testing.T) {
	d := New()
	if err := d.Validate(core.Config{}); err == nil {
		t.Error("paths should be required")
	}
	if err := d.Validate(core.Config{"paths": []string{"relative"}}); err == nil {
		t.Error("relative paths should be rejected")
	}
	if err := d.Validate(core.Config{"paths": []string{"/tmp"}, "exclude": []string{"[bad"}}); err == nil {
		t.Error("invalid glob should be rejected")
	}
	if err := d.Validate(core.Config{"paths": []string{"/tmp"}}); err != nil {
		t.Errorf("valid config rejected: %v", err)
	}
}

func TestBackupArchivesDirectory(t *testing.T) {
	root := tree(t)
	entries, err := archive(t, core.Config{"paths": []string{filepath.Join(root, "site")}})
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	if string(entries["site/index.html"]) != "<h1>hello</h1>" {
		t.Errorf("index.html missing or wrong: %q", entries["site/index.html"])
	}
	if _, ok := entries["site/assets/style.css"]; !ok {
		t.Errorf("nested file missing, got %v", names(entries))
	}
	if _, ok := entries["site/"]; !ok {
		t.Errorf("directory entry missing, got %v", names(entries))
	}
}

func TestExcludePatterns(t *testing.T) {
	root := tree(t)
	entries, err := archive(t, core.Config{
		"paths":   []string{filepath.Join(root, "site")},
		"exclude": []string{"**/node_modules/**", "*.log", "site/.env"},
	})
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	for _, unwanted := range []string{"site/app.log", "site/.env"} {
		if _, ok := entries[unwanted]; ok {
			t.Errorf("%s should have been excluded", unwanted)
		}
	}
	for name := range entries {
		if strings.Contains(name, "node_modules/left-pad") {
			t.Errorf("%s should have been excluded", name)
		}
	}
	if _, ok := entries["site/index.html"]; !ok {
		t.Error("index.html should still be archived")
	}
}

func TestExcludeDirectoryByName(t *testing.T) {
	root := tree(t)
	entries, err := archive(t, core.Config{
		"paths":   []string{filepath.Join(root, "site")},
		"exclude": []string{"node_modules"},
	})
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	for name := range entries {
		if strings.Contains(name, "node_modules") {
			t.Errorf("%s should have been excluded", name)
		}
	}
}

func TestBaseDirControlsEntryNames(t *testing.T) {
	root := tree(t)
	entries, err := archive(t, core.Config{
		"paths":    []string{filepath.Join(root, "site", "assets")},
		"base_dir": filepath.Join(root, "site"),
	})
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	if _, ok := entries["assets/style.css"]; !ok {
		t.Errorf("expected assets/style.css, got %v", names(entries))
	}
}

func TestSingleFilePath(t *testing.T) {
	root := tree(t)
	entries, err := archive(t, core.Config{"paths": []string{filepath.Join(root, "site", "index.html")}})
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one entry, got %v", names(entries))
	}
	if _, ok := entries["index.html"]; !ok {
		t.Errorf("got %v", names(entries))
	}
}

func TestSymlinksAreStoredAsLinksByDefault(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "real.txt"), []byte("real"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.txt", filepath.Join(dir, "link.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	st, err := New().Backup(t.Context(), core.Config{"paths": []string{dir}}, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	var linkType byte
	tr := tar.NewReader(st.Reader)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Name == "data/link.txt" {
			linkType = hdr.Typeflag
			if hdr.Linkname != "real.txt" {
				t.Errorf("linkname %q", hdr.Linkname)
			}
		}
		_, _ = io.Copy(io.Discard, tr)
	}
	if err := st.Reader.Close(); err != nil {
		t.Fatal(err)
	}
	if linkType != tar.TypeSymlink {
		t.Errorf("symlink stored with type %d", linkType)
	}
}

func TestFollowSymlinksArchivesContent(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "outside.txt"), []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "outside.txt"), filepath.Join(dir, "link.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	entries, err := archive(t, core.Config{"paths": []string{dir}, "follow_symlinks": true})
	if err != nil {
		t.Fatal(err)
	}
	if string(entries["data/link.txt"]) != "outside" {
		t.Errorf("followed symlink content %q", entries["data/link.txt"])
	}
}

func TestStrictModeFailsOnUnreadableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, permissions are not enforced")
	}
	root := t.TempDir()
	locked := filepath.Join(root, "locked.txt")
	if err := os.WriteFile(locked, []byte("secret"), 0o000); err != nil {
		t.Fatal(err)
	}
	if _, err := archive(t, core.Config{"paths": []string{root}, "strict": true}); err == nil {
		t.Error("strict mode should fail on an unreadable file")
	}
	entries, err := archive(t, core.Config{"paths": []string{root}})
	if err != nil {
		t.Fatalf("non-strict mode should skip the file: %v", err)
	}
	if _, ok := entries[filepath.Base(root)+"/locked.txt"]; ok {
		t.Error("unreadable file should have been skipped")
	}
}

func TestCloseBeforeEOFReportsAnError(t *testing.T) {
	root := tree(t)
	st, err := New().Backup(t.Context(), core.Config{"paths": []string{filepath.Join(root, "site")}}, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 8)
	_, _ = io.ReadFull(st.Reader, buf)
	if err := st.Reader.Close(); err == nil {
		t.Error("closing before the archive is complete should report an error")
	}
}

func TestRestoreRoundTrip(t *testing.T) {
	root := tree(t)
	st, err := New().Backup(t.Context(), core.Config{"paths": []string{filepath.Join(root, "site")}}, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, st.Reader); err != nil {
		t.Fatal(err)
	}
	if err := st.Reader.Close(); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "restored")
	d := New()
	if err := d.Restore(t.Context(), core.Config{}, &buf, source.RestoreOptions{
		Extension: "tar", TargetPath: target, Overwrite: true,
	}, testLogger()); err != nil {
		t.Fatalf("restore: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(target, "site", "index.html"))
	if err != nil {
		t.Fatalf("restored file missing: %v", err)
	}
	if string(got) != "<h1>hello</h1>" {
		t.Errorf("restored content %q", got)
	}
	info, err := os.Stat(filepath.Join(target, "site", "assets", "style.css"))
	if err != nil {
		t.Fatalf("nested restored file missing: %v", err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("restored mode %v", info.Mode().Perm())
	}
}

func TestRestoreRefusesPathTraversal(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	body := []byte("pwned")
	if err := tw.WriteHeader(&tar.Header{Name: "../escape.txt", Mode: 0o644, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	target := filepath.Join(t.TempDir(), "restored")
	err := New().Restore(t.Context(), core.Config{}, &buf, source.RestoreOptions{TargetPath: target}, testLogger())
	if err == nil {
		t.Fatal("path traversal should be refused")
	}
	if !strings.Contains(err.Error(), "escapes") {
		t.Errorf("unexpected error %v", err)
	}
}

func TestRestoreRefusesAbsolutePaths(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{Name: "/etc/cron.d/evil", Mode: 0o644, Size: 0}); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	target := filepath.Join(t.TempDir(), "restored")
	if err := New().Restore(t.Context(), core.Config{}, &buf, source.RestoreOptions{TargetPath: target}, testLogger()); err == nil {
		t.Fatal("absolute paths should be refused")
	}
}

func TestRestoreRefusesEscapingSymlink(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{
		Name: "link", Typeflag: tar.TypeSymlink, Linkname: "../../../../etc/passwd", Mode: 0o777,
	}); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	target := filepath.Join(t.TempDir(), "restored")
	if err := New().Restore(t.Context(), core.Config{}, &buf, source.RestoreOptions{TargetPath: target}, testLogger()); err == nil {
		t.Fatal("escaping symlink should be refused")
	}
}

func TestTestReportsMissingPaths(t *testing.T) {
	if err := New().Test(t.Context(), core.Config{"paths": []string{"/definitely/not/here"}}, testLogger()); err == nil {
		t.Error("missing paths should be reported")
	}
	root := tree(t)
	if err := New().Test(t.Context(), core.Config{"paths": []string{filepath.Join(root, "site")}}, testLogger()); err != nil {
		t.Errorf("readable paths should pass: %v", err)
	}
}
