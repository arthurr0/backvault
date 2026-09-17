package mongodb

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/source"
)

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func requireMongo(t *testing.T) {
	t.Helper()
	requireDocker(t)
	requireTools(t, []string{"mongodump"}, []string{"mongorestore"})
}

func mongoImage() string {
	if img := os.Getenv("BACKVAULT_TEST_MONGO_IMAGE"); img != "" {
		return img
	}
	return "mongo:7"
}

func startMongo(t *testing.T) (string, string) {
	t.Helper()
	port := freePort(t)
	name := startContainer(t, "backvault-mongo-", "-p", publish(port, 27017), mongoImage())
	uri := fmt.Sprintf("mongodb://127.0.0.1:%d/?directConnection=true", port)
	waitForMongo(t, uri)
	return name, uri
}

func waitForMongo(t *testing.T, uri string) {
	t.Helper()
	deadline := time.Now().Add(120 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		err := New().Test(ctx, core.Config{"uri": uri}, testLogger())
		cancel()
		if err == nil {
			return
		}
		last = err
		time.Sleep(time.Second)
	}
	t.Fatalf("mongo did not become ready: %v", last)
}

func mongoEval(t *testing.T, name, script string) string {
	t.Helper()
	out, err := exec.Command(containerBin, "exec", name, "mongosh", "--quiet", "--eval", script).CombinedOutput()
	if err != nil {
		t.Fatalf("mongosh %q: %v: %s", script, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestMongoDumpAndRestore(t *testing.T) {
	requireMongo(t)
	name, uri := startMongo(t)
	mongoEval(t, name, `db.getSiblingDB("app").notes.insertMany([{body:"one"},{body:"two"},{body:"three"}])`)

	cfg := core.Config{"uri": uri, "database": "app"}
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
	if st.Extension != "archive" {
		t.Errorf("extension %q", st.Extension)
	}
	if len(data) == 0 {
		t.Fatal("the archive is empty")
	}

	mongoEval(t, name, `db.getSiblingDB("app").notes.drop()`)
	if err := New().Restore(t.Context(), cfg, bytes.NewReader(data), source.RestoreOptions{
		Extension: "archive", Params: core.Config{"drop": true},
	}, testLogger()); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got := mongoEval(t, name, `db.getSiblingDB("app").notes.countDocuments({})`); got != "3" {
		t.Errorf("restored documents %q, want 3", got)
	}
}

func TestMongoUriIsNeverLogged(t *testing.T) {
	requireMongo(t)
	_, uri := startMongo(t)
	cfg := core.Config{"uri": uri + "&appName=backvault", "database": "nope"}
	st, err := New().Backup(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	_, _ = io.ReadAll(st.Reader)
	if err := st.Reader.Close(); err != nil && strings.Contains(err.Error(), uri) {
		t.Errorf("the connection uri leaked into the error: %v", err)
	}
}

func TestMongoBadUriFailsTest(t *testing.T) {
	requireMongo(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	err := New().Test(ctx, core.Config{
		"uri": "mongodb://127.0.0.1:1/?serverSelectionTimeoutMS=2000&directConnection=true",
	}, testLogger())
	if err == nil {
		t.Error("an unreachable server should fail the connection test")
	}
}
