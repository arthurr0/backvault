package redis

import (
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
)

const redisPassword = "backvaultredispass"

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func requireRedis(t *testing.T) {
	t.Helper()
	requireDocker(t)
	requireTools(t, []string{"redis-cli"})
}

func redisImage() string {
	if img := os.Getenv("BACKVAULT_TEST_REDIS_IMAGE"); img != "" {
		return img
	}
	return "redis:7"
}

func startRedis(t *testing.T) int {
	t.Helper()
	port := freePort(t)
	startContainer(t, "backvault-redis-",
		"-p", publish(port, 6379),
		redisImage(), "redis-server", "--requirepass", redisPassword)
	deadline := time.Now().Add(90 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		if err := New().Test(t.Context(), baseConfig(port), testLogger()); err == nil {
			return port
		} else {
			last = err
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("redis did not become ready: %v", last)
	return port
}

func baseConfig(port int) core.Config {
	return core.Config{"host": "127.0.0.1", "port": port, "password": redisPassword}
}

func TestRedisRdbSnapshot(t *testing.T) {
	requireRedis(t)
	port := startRedis(t)
	cmd := exec.Command("redis-cli", "-h", "127.0.0.1", "-p", strconv.Itoa(port), "--no-auth-warning", "set", "backvault", "works")
	cmd.Env = append(os.Environ(), "REDISCLI_AUTH="+redisPassword)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("seed redis: %v: %s", err, out)
	}

	st, err := New().Backup(t.Context(), baseConfig(port), testLogger())
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
	if st.Extension != "rdb" {
		t.Errorf("extension %q", st.Extension)
	}
	if !bytes.HasPrefix(data, []byte("REDIS")) {
		t.Fatalf("the snapshot is not an rdb file: %q", data[:min(8, len(data))])
	}
	if !bytes.Contains(data, []byte("backvault")) {
		t.Error("the snapshot does not contain the seeded key")
	}
}

func TestRedisWrongPassword(t *testing.T) {
	requireRedis(t)
	port := startRedis(t)
	cfg := baseConfig(port)
	cfg["password"] = "wrong"
	if err := New().Test(t.Context(), cfg, testLogger()); err == nil {
		t.Error("a wrong password should fail the connection test")
	}
	st, err := New().Backup(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	_, _ = io.ReadAll(st.Reader)
	err = st.Reader.Close()
	if err == nil {
		t.Fatal("a failing snapshot must report an error from Close")
	}
	if strings.Contains(err.Error(), "wrong") && strings.Contains(err.Error(), redisPassword) {
		t.Errorf("the password leaked into the error: %v", err)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
