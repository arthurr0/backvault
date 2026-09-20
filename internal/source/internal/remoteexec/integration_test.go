package remoteexec_test

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/source"
	"github.com/arthurr0/backvault/internal/source/command"
	"github.com/arthurr0/backvault/internal/source/docker"
	"github.com/arthurr0/backvault/internal/source/files"
	"github.com/arthurr0/backvault/internal/source/sqlite"

	cryptossh "golang.org/x/crypto/ssh"
)

var (
	containerOnce sync.Once
	containerBin  string
)

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func detectContainerCLI() {
	candidates := []string{"docker", "podman"}
	if forced := strings.TrimSpace(os.Getenv("BACKVAULT_TEST_CONTAINER_CLI")); forced != "" {
		candidates = []string{forced}
	}
	for _, name := range candidates {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err = exec.CommandContext(ctx, path, "ps", "--quiet").Run()
		cancel()
		if err == nil {
			containerBin = path
			return
		}
	}
}

func requireDocker(t *testing.T) {
	t.Helper()
	if os.Getenv("BACKVAULT_TEST_DOCKER") != "1" {
		t.Skip("set BACKVAULT_TEST_DOCKER=1 to run tests that need containers")
	}
	containerOnce.Do(detectContainerCLI)
	if containerBin == "" {
		t.Skip("neither docker nor podman has a usable daemon on this host")
	}
}

func startContainer(t *testing.T, prefix string, args ...string) string {
	t.Helper()
	name := prefix + strconv.FormatInt(time.Now().UnixNano(), 36)
	full := append([]string{"run", "-d", "--rm", "--name", name}, args...)
	out, err := exec.Command(containerBin, full...).CombinedOutput()
	if err != nil {
		t.Fatalf("start %s: %v: %s", name, err, out)
	}
	t.Cleanup(func() {
		if t.Failed() {
			logs, _ := exec.Command(containerBin, "logs", "--tail", "30", name).CombinedOutput()
			t.Logf("%s logs:\n%s", name, logs)
		}
		_ = exec.Command(containerBin, "rm", "-f", name).Run()
	})
	return name
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func alpineImage() string {
	if img := os.Getenv("BACKVAULT_TEST_ALPINE_IMAGE"); img != "" {
		return img
	}
	return "alpine:3.20"
}

func keyPair(t *testing.T) (privatePEM, authorized string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := cryptossh.MarshalPrivateKey(priv, "backvault-test")
	if err != nil {
		t.Fatal(err)
	}
	sshPub, err := cryptossh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(block)), strings.TrimSpace(string(cryptossh.MarshalAuthorizedKey(sshPub)))
}

func startHost(t *testing.T) core.Host {
	t.Helper()
	private, authorized := keyPair(t)
	port := freePort(t)
	script := strings.Join([]string{
		"apk add --no-cache openssh sqlite >/dev/null 2>&1",
		"ssh-keygen -A >/dev/null 2>&1",
		"mkdir -p /root/.ssh",
		"printf '%s\\n' \"$BACKVAULT_AUTHORIZED_KEY\" > /root/.ssh/authorized_keys",
		"chmod 700 /root/.ssh; chmod 600 /root/.ssh/authorized_keys",
		"mkdir -p '/data/sub dir' /restore /work",
		"printf 'payload one' > /data/one.txt",
		"printf 'payload two' > '/data/sub dir/two.txt'",
		"printf 'ignored' > /data/skip.log",
		"sqlite3 /data/app.db \"create table t(id integer primary key, name text); insert into t(name) values ('alpha'),('beta');\"",
		"exec /usr/sbin/sshd -D -e",
	}, "; ")
	startContainer(t, "backvault-remote-",
		"-p", fmt.Sprintf("127.0.0.1:%d:22", port),
		"-e", "BACKVAULT_AUTHORIZED_KEY="+authorized,
		alpineImage(), "/bin/sh", "-c", script)
	waitForSSH(t, port)
	return core.Host{
		Name: "test-host", Address: "127.0.0.1", Port: port, User: "root",
		Auth: core.HostAuthKey, PrivateKey: private, ConnectTimeout: 15,
	}
}

func waitForSSH(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(180 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
		if err == nil {
			banner := make([]byte, 4)
			_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, _ := conn.Read(banner)
			conn.Close()
			if n > 0 && string(banner[:n]) == "SSH-" {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("no ssh server on port %d", port)
}

func readAll(t *testing.T, st *source.Stream) []byte {
	t.Helper()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, st.Reader); err != nil {
		t.Fatalf("read stream: %v", err)
	}
	if err := st.Reader.Close(); err != nil {
		t.Fatalf("close stream: %v", err)
	}
	return buf.Bytes()
}

func tarEntries(t *testing.T, data []byte) map[string]string {
	t.Helper()
	found := map[string]string{}
	tr := tar.NewReader(bytes.NewReader(data))
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
	return found
}

func runOnHost(t *testing.T, host core.Host, script string) string {
	t.Helper()
	cfg := core.Config{"command": script, "extension": "txt"}.WithHost(&host)
	st, err := command.New().Backup(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatalf("run %q: %v", script, err)
	}
	return string(readAll(t, st))
}

func TestFilesOnHostRoundTrip(t *testing.T) {
	requireDocker(t)
	host := startHost(t)
	cfg := core.Config{
		"paths":    []string{"/data"},
		"base_dir": "/data",
		"exclude":  []string{"*.log"},
	}.WithHost(&host)

	if err := files.New().Test(t.Context(), cfg, testLogger()); err != nil {
		t.Fatalf("test: %v", err)
	}
	missing := core.Config{"paths": []string{"/data/nowhere"}}.WithHost(&host)
	if err := files.New().Test(t.Context(), missing, testLogger()); err == nil {
		t.Error("an unreadable path should be reported")
	}

	st, err := files.New().Backup(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	if st.Extension != "tar" {
		t.Errorf("extension %q", st.Extension)
	}
	archive := readAll(t, st)
	entries := tarEntries(t, archive)
	if entries["one.txt"] != "payload one" || entries["sub dir/two.txt"] != "payload two" {
		t.Errorf("archive contents %v", entries)
	}
	if _, excluded := entries["skip.log"]; excluded {
		t.Error("the exclude pattern was not applied")
	}

	if err := files.New().Restore(t.Context(), cfg, bytes.NewReader(archive),
		source.RestoreOptions{Extension: "tar", TargetPath: "/restore"}, testLogger()); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got := runOnHost(t, host, "cat '/restore/sub dir/two.txt'"); got != "payload two" {
		t.Errorf("restored file %q", got)
	}
}

func TestCommandOnHost(t *testing.T) {
	requireDocker(t)
	host := startHost(t)
	cfg := core.Config{
		"command":     "printf '%s:%s' \"$BACKVAULT_TEST\" \"$(pwd)\"",
		"env":         []string{"BACKVAULT_TEST=value"},
		"working_dir": "/work",
		"extension":   "txt",
	}.WithHost(&host)

	if err := command.New().Test(t.Context(), cfg, testLogger()); err != nil {
		t.Fatalf("test: %v", err)
	}
	st, err := command.New().Backup(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	if got := string(readAll(t, st)); got != "value:/work" {
		t.Errorf("got %q", got)
	}

	failing := core.Config{"command": "printf partial; echo 'remote is unhappy' >&2; exit 7"}.WithHost(&host)
	st, err = command.New().Backup(t.Context(), failing, testLogger())
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	if _, err := io.ReadAll(st.Reader); err != nil {
		t.Fatal(err)
	}
	err = st.Reader.Close()
	if err == nil {
		t.Fatal("a failing remote command must report an error from Close")
	}
	if !strings.Contains(err.Error(), "code 7") || !strings.Contains(err.Error(), "remote is unhappy") {
		t.Errorf("error is missing detail: %v", err)
	}

	restore := core.Config{"restore_command": "cat > /restore/from-stdin.txt"}.WithHost(&host)
	if err := command.New().Restore(t.Context(), restore, strings.NewReader("restored payload"),
		source.RestoreOptions{}, testLogger()); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got := runOnHost(t, host, "cat /restore/from-stdin.txt"); got != "restored payload" {
		t.Errorf("restored content %q", got)
	}
}

func TestSQLiteOnHost(t *testing.T) {
	requireDocker(t)
	host := startHost(t)
	cfg := core.Config{"path": "/data/app.db"}.WithHost(&host)

	if err := sqlite.New().Test(t.Context(), cfg, testLogger()); err != nil {
		t.Fatalf("test: %v", err)
	}
	st, err := sqlite.New().Backup(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	copied := readAll(t, st)
	if !bytes.HasPrefix(copied, []byte("SQLite format 3")) {
		t.Fatalf("the copy is not a sqlite database: %q", copied[:min(16, len(copied))])
	}
	if st.Extension != "sqlite" {
		t.Errorf("extension %q", st.Extension)
	}

	target := "/restore/app.db"
	if err := sqlite.New().Restore(t.Context(), cfg, bytes.NewReader(copied),
		source.RestoreOptions{TargetPath: target, Extension: "sqlite"}, testLogger()); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got := strings.TrimSpace(runOnHost(t, host, "sqlite3 /restore/app.db 'select count(*) from t'")); got != "2" {
		t.Errorf("restored row count %q", got)
	}
	if err := sqlite.New().Restore(t.Context(), cfg, bytes.NewReader(copied),
		source.RestoreOptions{TargetPath: target, Extension: "sqlite"}, testLogger()); err == nil {
		t.Error("restoring over an existing file without overwrite should fail")
	}
}

func TestDockerOnHost(t *testing.T) {
	requireDocker(t)
	host := startHost(t)
	if strings.TrimSpace(runOnHost(t, host, "command -v docker || command -v podman || true")) == "" {
		t.Skip("the test host has no container engine, nothing to exercise")
	}
	cfg := core.Config{"mode": "volume", "volume": "backvault-remote-test"}.WithHost(&host)
	if err := docker.New().Test(t.Context(), cfg, testLogger()); err != nil {
		t.Skipf("no usable container engine on the test host: %v", err)
	}
	st, err := docker.New().Backup(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	readAll(t, st)
}
