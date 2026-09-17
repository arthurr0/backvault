package mysql

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
	"github.com/arthurr0/backvault/internal/source"
)

const mysqlPassword = "backvaultmysqlpass"

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func requireMySQL(t *testing.T) {
	t.Helper()
	requireDocker(t)
	requireTools(t, []string{"mysqldump", "mariadb-dump"}, []string{"mysql", "mariadb"})
}

func mysqlImage() string {
	if img := os.Getenv("BACKVAULT_TEST_MYSQL_IMAGE"); img != "" {
		return img
	}
	return "mariadb:11"
}

func startMySQL(t *testing.T) int {
	t.Helper()
	port := freePort(t)
	startContainer(t, "backvault-mysql-",
		"-p", publish(port, 3306),
		"-e", "MARIADB_ROOT_PASSWORD="+mysqlPassword,
		"-e", "MYSQL_ROOT_PASSWORD="+mysqlPassword,
		"-e", "MARIADB_DATABASE=app",
		"-e", "MYSQL_DATABASE=app",
		mysqlImage())
	waitForMySQL(t, port)
	return port
}

func baseConfig(port int) core.Config {
	return core.Config{
		"host": "127.0.0.1", "port": port, "user": "root",
		"password": mysqlPassword, "databases": []string{"app"},
	}
}

func waitForMySQL(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(180 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		if err := New().Test(t.Context(), baseConfig(port), testLogger()); err == nil {
			return
		} else {
			last = err
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("mysql did not become ready: %v", last)
}

func query(t *testing.T, port int, db, statement string) string {
	t.Helper()
	bin, err := exec.LookPath("mysql")
	if err != nil {
		bin, err = exec.LookPath("mariadb")
		if err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(bin, "--protocol=TCP", "-h", "127.0.0.1", "-P", strconv.Itoa(port),
		"-u", "root", "-p"+mysqlPassword, "--batch", "--skip-column-names", "-e", statement, db)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("mysql %q: %v: %s", statement, err, out)
	}
	return strings.TrimSpace(string(out))
}

func seed(t *testing.T, port int) {
	t.Helper()
	query(t, port, "app", "create table notes (id int auto_increment primary key, body varchar(255))")
	query(t, port, "app", "insert into notes (body) values ('one'), ('two'), ('three')")
}

func TestMySQLDumpAndRestore(t *testing.T) {
	requireMySQL(t)
	port := startMySQL(t)
	seed(t, port)

	cfg := baseConfig(port)
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
	if st.Extension != "sql" {
		t.Errorf("extension %q", st.Extension)
	}
	if !bytes.Contains(data, []byte("CREATE TABLE `notes`")) {
		t.Error("the dump does not contain the table definition")
	}

	query(t, port, "app", "create database restored")
	restoreCfg := baseConfig(port)
	if err := New().Restore(t.Context(), restoreCfg, bytes.NewReader(data), source.RestoreOptions{
		Extension: "sql", Params: core.Config{"database": "restored"},
	}, testLogger()); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got := query(t, port, "restored", "select count(*) from notes"); got != "3" {
		t.Errorf("restored rows %q, want 3", got)
	}
}

func TestMySQLAllDatabases(t *testing.T) {
	requireMySQL(t)
	port := startMySQL(t)
	seed(t, port)
	cfg := baseConfig(port)
	cfg["all_databases"] = true
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
	if !bytes.Contains(data, []byte("CREATE DATABASE")) {
		t.Error("a full server dump should create the databases")
	}
}

func TestMySQLFailureIsReportedAndPasswordStaysSecret(t *testing.T) {
	requireMySQL(t)
	port := startMySQL(t)
	cfg := baseConfig(port)
	cfg["databases"] = []string{"nope"}
	st, err := New().Backup(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	_, _ = io.ReadAll(st.Reader)
	err = st.Reader.Close()
	if err == nil {
		t.Fatal("a failing dump must report an error from Close")
	}
	if strings.Contains(err.Error(), mysqlPassword) {
		t.Errorf("the password leaked into the error: %v", err)
	}
}

func TestMySQLWrongPasswordFailsTest(t *testing.T) {
	requireMySQL(t)
	port := startMySQL(t)
	cfg := baseConfig(port)
	cfg["password"] = "wrong"
	if err := New().Test(t.Context(), cfg, testLogger()); err == nil {
		t.Error("a wrong password should fail the connection test")
	}
}
