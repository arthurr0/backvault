package ssh

import (
	"archive/tar"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/source"

	cryptossh "golang.org/x/crypto/ssh"
)

const sshPassword = "backvaultpass"

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func sshImage() string {
	if img := os.Getenv("BACKVAULT_TEST_ALPINE_IMAGE"); img != "" {
		return img
	}
	return "alpine:3.20"
}

func keyPair(t *testing.T) (privatePEM string, authorized string) {
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

func startSSH(t *testing.T, authorizedKey string) int {
	t.Helper()
	port := freePort(t)
	script := strings.Join([]string{
		"apk add --no-cache openssh >/dev/null 2>&1",
		"ssh-keygen -A >/dev/null 2>&1",
		"echo 'root:" + sshPassword + "' | chpasswd",
		"sed -i 's/^#*PermitRootLogin.*/PermitRootLogin yes/' /etc/ssh/sshd_config",
		"mkdir -p /root/.ssh",
		"printf '%s\\n' \"$BACKVAULT_AUTHORIZED_KEY\" > /root/.ssh/authorized_keys",
		"chmod 700 /root/.ssh; chmod 600 /root/.ssh/authorized_keys",
		"mkdir -p /data/sub /restore",
		"printf 'payload one' > /data/one.txt",
		"printf 'payload two' > /data/sub/two.txt",
		"exec /usr/sbin/sshd -D -e",
	}, "; ")
	startContainer(t, "backvault-sshd-",
		"-p", publish(port, 22),
		"-e", "BACKVAULT_AUTHORIZED_KEY="+authorizedKey,
		sshImage(), "/bin/sh", "-c", script)
	waitForSSH(t, port)
	return port
}

func waitForSSH(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(120 * time.Second)
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

func passwordConfig(port int) core.Config {
	return core.Config{
		"host": "127.0.0.1", "port": port, "user": "root",
		"auth": "password", "password": sshPassword,
		"command": "tar -C /data -cf - .", "extension": "tar",
	}
}

func TestSSHTestConnection(t *testing.T) {
	requireDocker(t)
	_, authorized := keyPair(t)
	port := startSSH(t, authorized)
	if err := New().Test(t.Context(), passwordConfig(port), testLogger()); err != nil {
		t.Fatalf("test: %v", err)
	}
	bad := passwordConfig(port)
	bad["password"] = "wrong"
	if err := New().Test(t.Context(), bad, testLogger()); err == nil {
		t.Error("a wrong password should fail")
	}
}

func TestSSHKeyAuthentication(t *testing.T) {
	requireDocker(t)
	private, authorized := keyPair(t)
	port := startSSH(t, authorized)
	cfg := core.Config{
		"host": "127.0.0.1", "port": port, "user": "root",
		"auth": "key", "private_key": private,
		"command": "printf 'from the key session'", "extension": "txt",
	}
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
	if string(data) != "from the key session" {
		t.Errorf("got %q", data)
	}
}

func TestSSHStreamsRemoteTar(t *testing.T) {
	requireDocker(t)
	_, authorized := keyPair(t)
	port := startSSH(t, authorized)
	st, err := New().Backup(t.Context(), passwordConfig(port), testLogger())
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	var archive bytes.Buffer
	if _, err := io.Copy(&archive, st.Reader); err != nil {
		t.Fatal(err)
	}
	if err := st.Reader.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if st.Extension != "tar" {
		t.Errorf("extension %q", st.Extension)
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
	if found["one.txt"] != "payload one" || found["sub/two.txt"] != "payload two" {
		t.Errorf("remote archive contents %v", found)
	}
}

func TestSSHFailingCommandIsReportedOnClose(t *testing.T) {
	requireDocker(t)
	_, authorized := keyPair(t)
	port := startSSH(t, authorized)
	cfg := passwordConfig(port)
	cfg["command"] = "printf partial; echo 'remote is unhappy' >&2; exit 7"
	st, err := New().Backup(t.Context(), cfg, testLogger())
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
	if !strings.Contains(err.Error(), "code 7") {
		t.Errorf("error does not mention the exit code: %v", err)
	}
	if !strings.Contains(err.Error(), "remote is unhappy") {
		t.Errorf("error does not include the remote stderr: %v", err)
	}
	if strings.Contains(err.Error(), sshPassword) {
		t.Errorf("the password leaked into the error: %v", err)
	}
}

func TestSSHRestoreCommand(t *testing.T) {
	requireDocker(t)
	_, authorized := keyPair(t)
	port := startSSH(t, authorized)
	cfg := passwordConfig(port)
	cfg["restore_command"] = "tar -C /restore -xf -"

	st, err := New().Backup(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	var archive bytes.Buffer
	if _, err := io.Copy(&archive, st.Reader); err != nil {
		t.Fatal(err)
	}
	if err := st.Reader.Close(); err != nil {
		t.Fatal(err)
	}

	if err := New().Restore(t.Context(), cfg, bytes.NewReader(archive.Bytes()),
		source.RestoreOptions{Extension: "tar"}, testLogger()); err != nil {
		t.Fatalf("restore: %v", err)
	}

	check := passwordConfig(port)
	check["command"] = "cat /restore/sub/two.txt"
	verify, err := New().Backup(t.Context(), check, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(verify.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := verify.Reader.Close(); err != nil {
		t.Fatal(err)
	}
	if string(got) != "payload two" {
		t.Errorf("restored remote file %q", got)
	}

	noRestore := passwordConfig(port)
	if err := New().Restore(t.Context(), noRestore, bytes.NewReader(nil),
		source.RestoreOptions{}, testLogger()); err == nil {
		t.Error("a source without a restore command should refuse to restore")
	}
}

func TestSSHHostKeyPinning(t *testing.T) {
	requireDocker(t)
	_, authorized := keyPair(t)
	port := startSSH(t, authorized)
	cfg := passwordConfig(port)
	cfg["host_key"] = "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if err := New().Test(t.Context(), cfg, testLogger()); err == nil {
		t.Fatal("a mismatching host key must be refused")
	}

	var fingerprint string
	client, err := cryptossh.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port), &cryptossh.ClientConfig{
		User: "root",
		Auth: []cryptossh.AuthMethod{cryptossh.Password(sshPassword)},
		HostKeyCallback: func(hostname string, remote net.Addr, key cryptossh.PublicKey) error {
			fingerprint = cryptossh.FingerprintSHA256(key)
			return nil
		},
		Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	client.Close()

	cfg["host_key"] = fingerprint
	if err := New().Test(t.Context(), cfg, testLogger()); err != nil {
		t.Fatalf("the correct host key should be accepted: %v", err)
	}
}
