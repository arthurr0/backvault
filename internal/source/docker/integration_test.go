package docker

import (
	"archive/tar"
	"bytes"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/source"
)

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func helperImage() string {
	if img := os.Getenv("BACKVAULT_TEST_ALPINE_IMAGE"); img != "" {
		return img
	}
	return "alpine:3.20"
}

func createVolume(t *testing.T, contents map[string]string) string {
	t.Helper()
	name := "backvault-vol-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	container(t, "volume", "create", name)
	t.Cleanup(func() { _ = exec.Command(containerBin, "volume", "rm", "-f", name).Run() })
	var script strings.Builder
	for path, body := range contents {
		script.WriteString("mkdir -p /data/$(dirname " + path + "); ")
		script.WriteString("printf '%s' '" + body + "' > /data/" + path + "; ")
	}
	script.WriteString("true")
	container(t, "run", "--rm", "-v", name+":/data", helperImage(), "/bin/sh", "-c", script.String())
	return name
}

func readVolume(t *testing.T, volume, path string) string {
	t.Helper()
	return container(t, "run", "--rm", "-v", volume+":/data:ro", helperImage(), "cat", "/data/"+path)
}

func TestDockerVolumeBackupAndRestore(t *testing.T) {
	requireDocker(t)
	volume := createVolume(t, map[string]string{
		"config.yaml":  "listen: 8080",
		"sub/data.txt": "hello from the volume",
	})

	cfg := core.Config{"mode": "volume", "volume": volume, "image": helperImage(), "binary_path": dockerBinaryPath(t)}
	if err := New().Test(t.Context(), cfg, testLogger()); err != nil {
		t.Fatalf("test: %v", err)
	}

	st, err := New().Backup(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	if st.Extension != "tar" {
		t.Errorf("extension %q, want tar", st.Extension)
	}
	var archive bytes.Buffer
	if _, err := io.Copy(&archive, st.Reader); err != nil {
		t.Fatalf("read archive: %v", err)
	}
	if err := st.Reader.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	found := map[string]string{}
	tr := tar.NewReader(bytes.NewReader(archive.Bytes()))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read tar: %v", err)
		}
		if hdr.Typeflag == tar.TypeReg {
			body, _ := io.ReadAll(tr)
			found[strings.TrimPrefix(hdr.Name, "./")] = string(body)
		}
	}
	if found["config.yaml"] != "listen: 8080" {
		t.Errorf("config.yaml in the archive: %q", found["config.yaml"])
	}
	if found["sub/data.txt"] != "hello from the volume" {
		t.Errorf("sub/data.txt in the archive: %q", found["sub/data.txt"])
	}

	target := createVolume(t, map[string]string{"stale.txt": "should be gone"})
	restoreCfg := core.Config{"mode": "volume", "volume": target, "image": helperImage(), "binary_path": dockerBinaryPath(t)}
	if err := New().Restore(t.Context(), restoreCfg, bytes.NewReader(archive.Bytes()), source.RestoreOptions{
		Extension: "tar", Overwrite: true,
	}, testLogger()); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got := readVolume(t, target, "sub/data.txt"); got != "hello from the volume" {
		t.Errorf("restored file %q", got)
	}
	listing := container(t, "run", "--rm", "-v", target+":/data:ro", helperImage(), "ls", "/data")
	if strings.Contains(listing, "stale.txt") {
		t.Errorf("the volume was not cleared before the restore: %q", listing)
	}
}

func TestDockerExecMode(t *testing.T) {
	requireDocker(t)
	name := startContainer(t, "backvault-exec-", helperImage(), "sleep", "300")

	cfg := core.Config{"mode": "exec", "container": name, "command": "printf 'dump from inside'",
		"extension": "sql", "binary_path": dockerBinaryPath(t)}
	if err := New().Test(t.Context(), cfg, testLogger()); err != nil {
		t.Fatalf("test: %v", err)
	}
	st, err := New().Backup(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	data, err := io.ReadAll(st.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Reader.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if string(data) != "dump from inside" {
		t.Errorf("got %q", data)
	}
	if st.Extension != "sql" {
		t.Errorf("extension %q", st.Extension)
	}
}

func TestDockerExecFailureIsReported(t *testing.T) {
	requireDocker(t)
	name := startContainer(t, "backvault-fail-", helperImage(), "sleep", "300")

	cfg := core.Config{"mode": "exec", "container": name, "command": "echo 'no backup today' >&2; exit 5",
		"binary_path": dockerBinaryPath(t)}
	st, err := New().Backup(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	_, _ = io.ReadAll(st.Reader)
	err = st.Reader.Close()
	if err == nil {
		t.Fatal("a failing command must report an error from Close")
	}
	if !strings.Contains(err.Error(), "no backup today") {
		t.Errorf("error is missing the container stderr: %v", err)
	}
}

func TestDockerMissingVolumeFailsTest(t *testing.T) {
	requireDocker(t)
	cfg := core.Config{"mode": "volume", "volume": "backvault-volume-that-does-not-exist",
		"image": helperImage(), "binary_path": dockerBinaryPath(t)}
	if err := New().Test(t.Context(), cfg, testLogger()); err == nil {
		t.Error("a missing volume should fail the test")
	}
}
