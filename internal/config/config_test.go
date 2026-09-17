package config

import (
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("BACKVAULT_CONFIG", "")
	dir := t.TempDir()
	cfg, err := Load(Flags{DataDir: dir})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Listen != DefaultListen {
		t.Fatalf("listen = %q", cfg.Listen)
	}
	if cfg.WorkDir != filepath.Join(dir, "work") {
		t.Fatalf("work dir = %q", cfg.WorkDir)
	}
	if cfg.MasterKeyFile != filepath.Join(dir, "master.key") {
		t.Fatalf("master key file = %q", cfg.MasterKeyFile)
	}
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}
}

func TestEnvAndFlagPrecedence(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "backvault.yaml")
	if err := writeFile(file, "listen: \":9000\"\nlog_level: debug\nbase_url: https://example.test/\n"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BACKVAULT_LISTEN", ":9100")
	t.Setenv("BACKVAULT_TRUSTED_PROXIES", "10.0.0.1, 10.0.0.2")
	cfg, err := Load(Flags{ConfigFile: file, DataDir: dir, Listen: ":9200"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Listen != ":9200" {
		t.Fatalf("flag should win, got %q", cfg.Listen)
	}
	if cfg.LogLevel != "debug" {
		t.Fatalf("log level = %q", cfg.LogLevel)
	}
	if cfg.BaseURL != "https://example.test" {
		t.Fatalf("base url = %q", cfg.BaseURL)
	}
	if len(cfg.TrustedProxies) != 2 {
		t.Fatalf("trusted proxies = %v", cfg.TrustedProxies)
	}
}

func TestValidate(t *testing.T) {
	cfg := Default()
	cfg.LogLevel = "loud"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for bad log level")
	}
	cfg = Default()
	cfg.BaseURL = "example.test"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for bad base url")
	}
	cfg = Default()
	cfg.AdminEmail = "a@b.c"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for admin email without password")
	}
}
