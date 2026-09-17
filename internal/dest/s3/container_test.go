package s3

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	containerOnce sync.Once
	containerBin  string
)

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

func requireTools(t *testing.T, groups ...[]string) {
	t.Helper()
	for _, group := range groups {
		found := false
		for _, name := range group {
			if _, err := exec.LookPath(name); err == nil {
				found = true
				break
			}
		}
		if !found {
			t.Skipf("none of %s is installed on this host", strings.Join(group, ", "))
		}
	}
}

func container(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command(containerBin, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v: %s", filepath.Base(containerBin), strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
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

func publish(port, containerPort int) string {
	return fmt.Sprintf("127.0.0.1:%d:%d", port, containerPort)
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

func waitForPort(t *testing.T, port int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("nothing is listening on port %d", port)
}
