package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	DefaultListen    = ":8080"
	DefaultDataDir   = "./data"
	DefaultLogLevel  = "info"
	DefaultLogFormat = "text"
)

type Config struct {
	Listen         string   `yaml:"listen"`
	DataDir        string   `yaml:"data_dir"`
	WorkDir        string   `yaml:"work_dir"`
	MasterKey      string   `yaml:"master_key"`
	MasterKeyFile  string   `yaml:"master_key_file"`
	BaseURL        string   `yaml:"base_url"`
	LogLevel       string   `yaml:"log_level"`
	LogFormat      string   `yaml:"log_format"`
	AdminEmail     string   `yaml:"admin_email"`
	AdminPassword  string   `yaml:"admin_password"`
	TrustedProxies []string `yaml:"trusted_proxies"`
	MetricsToken   string   `yaml:"metrics_token"`

	ConfigFile string `yaml:"-"`
}

type Flags struct {
	ConfigFile string
	Listen     string
	DataDir    string
	WorkDir    string
	BaseURL    string
	LogLevel   string
	LogFormat  string
}

func Default() Config {
	return Config{
		Listen:    DefaultListen,
		DataDir:   DefaultDataDir,
		LogLevel:  DefaultLogLevel,
		LogFormat: DefaultLogFormat,
	}
}

func Load(f Flags) (Config, error) {
	cfg := Default()

	path := strings.TrimSpace(f.ConfigFile)
	if path == "" {
		path = strings.TrimSpace(os.Getenv("BACKVAULT_CONFIG"))
	}
	if path != "" {
		if err := loadFile(&cfg, path); err != nil {
			return cfg, err
		}
		cfg.ConfigFile = path
	}

	applyEnv(&cfg)
	applyFlags(&cfg, f)

	if err := cfg.normalize(); err != nil {
		return cfg, err
	}
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func loadFile(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config file %s: %w", path, err)
	}
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(cfg); err != nil {
		return fmt.Errorf("parse config file %s: %w", path, err)
	}
	return nil
}

func applyEnv(cfg *Config) {
	setString(&cfg.Listen, "BACKVAULT_LISTEN")
	setString(&cfg.DataDir, "BACKVAULT_DATA_DIR")
	setString(&cfg.WorkDir, "BACKVAULT_WORK_DIR")
	setString(&cfg.MasterKey, "BACKVAULT_MASTER_KEY")
	setString(&cfg.MasterKeyFile, "BACKVAULT_MASTER_KEY_FILE")
	setString(&cfg.BaseURL, "BACKVAULT_BASE_URL")
	setString(&cfg.LogLevel, "BACKVAULT_LOG_LEVEL")
	setString(&cfg.LogFormat, "BACKVAULT_LOG_FORMAT")
	setString(&cfg.AdminEmail, "BACKVAULT_ADMIN_EMAIL")
	setString(&cfg.AdminPassword, "BACKVAULT_ADMIN_PASSWORD")
	setString(&cfg.MetricsToken, "BACKVAULT_METRICS_TOKEN")
	if v := strings.TrimSpace(os.Getenv("BACKVAULT_TRUSTED_PROXIES")); v != "" {
		cfg.TrustedProxies = splitList(v)
	}
}

func applyFlags(cfg *Config, f Flags) {
	setFlag(&cfg.Listen, f.Listen)
	setFlag(&cfg.DataDir, f.DataDir)
	setFlag(&cfg.WorkDir, f.WorkDir)
	setFlag(&cfg.BaseURL, f.BaseURL)
	setFlag(&cfg.LogLevel, f.LogLevel)
	setFlag(&cfg.LogFormat, f.LogFormat)
}

func setString(dst *string, env string) {
	if v, ok := os.LookupEnv(env); ok {
		if v = strings.TrimSpace(v); v != "" {
			*dst = v
		}
	}
}

func setFlag(dst *string, v string) {
	if v = strings.TrimSpace(v); v != "" {
		*dst = v
	}
}

func splitList(v string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' }) {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func (c *Config) normalize() error {
	c.Listen = strings.TrimSpace(c.Listen)
	if c.Listen == "" {
		c.Listen = DefaultListen
	}
	c.LogLevel = strings.ToLower(strings.TrimSpace(c.LogLevel))
	if c.LogLevel == "" {
		c.LogLevel = DefaultLogLevel
	}
	c.LogFormat = strings.ToLower(strings.TrimSpace(c.LogFormat))
	if c.LogFormat == "" {
		c.LogFormat = DefaultLogFormat
	}
	c.BaseURL = strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")

	dataDir := strings.TrimSpace(c.DataDir)
	if dataDir == "" {
		dataDir = DefaultDataDir
	}
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		return fmt.Errorf("resolve data dir %s: %w", dataDir, err)
	}
	c.DataDir = abs

	workDir := strings.TrimSpace(c.WorkDir)
	if workDir == "" {
		workDir = filepath.Join(c.DataDir, "work")
	}
	if abs, err = filepath.Abs(workDir); err != nil {
		return fmt.Errorf("resolve work dir %s: %w", workDir, err)
	}
	c.WorkDir = abs

	keyFile := strings.TrimSpace(c.MasterKeyFile)
	if keyFile == "" {
		keyFile = filepath.Join(c.DataDir, "master.key")
	}
	if abs, err = filepath.Abs(keyFile); err != nil {
		return fmt.Errorf("resolve master key file %s: %w", keyFile, err)
	}
	c.MasterKeyFile = abs
	return nil
}

func (c Config) Validate() error {
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("invalid log_level %q: want debug, info, warn or error", c.LogLevel)
	}
	switch c.LogFormat {
	case "text", "json":
	default:
		return fmt.Errorf("invalid log_format %q: want text or json", c.LogFormat)
	}
	if c.BaseURL != "" && !strings.HasPrefix(c.BaseURL, "http://") && !strings.HasPrefix(c.BaseURL, "https://") {
		return fmt.Errorf("invalid base_url %q: must start with http:// or https://", c.BaseURL)
	}
	if !strings.Contains(c.Listen, ":") {
		return fmt.Errorf("invalid listen address %q: want host:port", c.Listen)
	}
	if (c.AdminEmail == "") != (c.AdminPassword == "") {
		return fmt.Errorf("admin_email and admin_password must be set together")
	}
	return nil
}

func (c Config) DatabasePath() string {
	return filepath.Join(c.DataDir, "backvault.db")
}

func (c Config) EnsureDirs() error {
	for _, dir := range []string{c.DataDir, c.WorkDir, filepath.Dir(c.MasterKeyFile)} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
	}
	return nil
}

func (c Config) Summary() [][2]string {
	out := [][2]string{
		{"config file", orNone(c.ConfigFile)},
		{"listen", c.Listen},
		{"data dir", c.DataDir},
		{"work dir", c.WorkDir},
		{"database", c.DatabasePath()},
		{"master key file", c.MasterKeyFile},
		{"master key from env", yesNo(c.MasterKey != "")},
		{"base url", orNone(c.BaseURL)},
		{"log level", c.LogLevel},
		{"log format", c.LogFormat},
		{"trusted proxies", orNone(strings.Join(c.TrustedProxies, ", "))},
		{"metrics token", yesNo(c.MetricsToken != "")},
		{"bootstrap admin", orNone(c.AdminEmail)},
	}
	return out
}

func orNone(v string) string {
	if strings.TrimSpace(v) == "" {
		return "(none)"
	}
	return v
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
