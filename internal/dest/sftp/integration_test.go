package sftp

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/dest"
)

const (
	sftpUser = "backvault"
	sftpPass = "backvaultpass"
)

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func sftpImage() string {
	if img := os.Getenv("BACKVAULT_TEST_SFTP_IMAGE"); img != "" {
		return img
	}
	return "atmoz/sftp:alpine"
}

func startSFTP(t *testing.T) int {
	t.Helper()
	port := freePort(t)
	startContainer(t, "backvault-sftp-",
		"-p", publish(port, 22),
		sftpImage(),
		fmt.Sprintf("%s:%s:::backups", sftpUser, sftpPass))
	waitForPort(t, port, 60*time.Second)
	time.Sleep(time.Second)
	return port
}

func openSFTP(t *testing.T, port int, extra core.Config) dest.Client {
	t.Helper()
	cfg := core.Config{
		"host": "127.0.0.1", "port": port, "user": sftpUser,
		"auth": "password", "password": sftpPass, "base_path": "backups/backvault",
	}
	for k, v := range extra {
		cfg[k] = v
	}
	var client dest.Client
	var err error
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		client, err = New().Open(t.Context(), cfg, testLogger())
		if err == nil {
			t.Cleanup(func() { _ = client.Close() })
			return client
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("open: %v", err)
	return nil
}

func TestSFTPRoundTrip(t *testing.T) {
	requireDocker(t)
	port := startSFTP(t)
	client := openSFTP(t, port, nil)

	if err := client.Test(t.Context()); err != nil {
		t.Fatalf("test: %v", err)
	}

	payload := bytes.Repeat([]byte("backvault-sftp-"), 4096)
	name := "files-daily/files-daily-20240101-000000.tar"
	if err := client.Put(t.Context(), name, bytes.NewReader(payload), int64(len(payload))); err != nil {
		t.Fatalf("put: %v", err)
	}

	st, err := client.Stat(t.Context(), name)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if st.Size != int64(len(payload)) {
		t.Errorf("stat size %d, want %d", st.Size, len(payload))
	}
	if st.Path != name {
		t.Errorf("stat path %q", st.Path)
	}

	rc, err := client.Get(t.Context(), name)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	got, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Error("round trip mismatch")
	}

	items, err := client.List(t.Context(), "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var files []string
	for _, item := range items {
		if !item.IsDir {
			files = append(files, item.Path)
		}
	}
	if len(files) != 1 || files[0] != name {
		t.Errorf("list returned %v", files)
	}

	if err := client.Delete(t.Context(), name); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := client.Delete(t.Context(), name); err != nil {
		t.Errorf("deleting a missing object should return nil, got %v", err)
	}
	if _, err := client.Stat(t.Context(), name); err == nil {
		t.Error("the object should be gone")
	}
}

func TestSFTPOverwriteAndNoPartialsLeftBehind(t *testing.T) {
	requireDocker(t)
	port := startSFTP(t)
	client := openSFTP(t, port, nil)

	if err := client.Put(t.Context(), "job/a.bin", bytes.NewReader([]byte("first")), 5); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := client.Put(t.Context(), "job/a.bin", bytes.NewReader([]byte("second")), 6); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	rc, err := client.Get(t.Context(), "job/a.bin")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if string(got) != "second" {
		t.Errorf("got %q, want second", got)
	}
	items, err := client.List(t.Context(), "job")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if len(item.Path) > 8 && item.Path[len(item.Path)-8:] == ".partial" {
			t.Errorf("a partial upload was left behind: %s", item.Path)
		}
	}
}

func TestSFTPWrongPassword(t *testing.T) {
	requireDocker(t)
	port := startSFTP(t)
	_, err := New().Open(t.Context(), core.Config{
		"host": "127.0.0.1", "port": port, "user": sftpUser,
		"auth": "password", "password": "nope", "base_path": "backups",
	}, testLogger())
	if err == nil {
		t.Error("a wrong password should fail")
	}
}

func TestSFTPHostKeyMismatchIsRefused(t *testing.T) {
	requireDocker(t)
	port := startSFTP(t)
	_, err := New().Open(t.Context(), core.Config{
		"host": "127.0.0.1", "port": port, "user": sftpUser,
		"auth": "password", "password": sftpPass, "base_path": "backups",
		"host_key": "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
	}, testLogger())
	if err == nil {
		t.Error("a mismatching host key fingerprint should be refused")
	}
}
