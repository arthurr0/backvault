package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/dest"
	"github.com/arthurr0/backvault/internal/notify"
	"github.com/arthurr0/backvault/internal/source"
)

func init() {
	source.Register(&testSource{})
	source.Register(&testRemoteSource{})
	dest.Register(&testDestination{})
	notify.Register(&testNotifier{})
}

type testSource struct{}

func (t *testSource) Spec() core.DriverSpec {
	return core.DriverSpec{
		Kind:     "testsource",
		Label:    "Test source",
		Category: "test",
		Fields: []core.Field{
			{Name: "payload", Label: "Payload", Type: core.FieldString, Required: true},
			{Name: "password", Label: "Password", Type: core.FieldSecret, Secret: true},
		},
		Capabilities: []string{core.CapTest},
		Tools:        []string{"testtool"},
	}
}

func (t *testSource) Validate(cfg core.Config) error {
	if cfg.String("payload") == "" {
		return errors.New("payload is required")
	}
	return nil
}

func (t *testSource) Test(ctx context.Context, cfg core.Config, log *slog.Logger) error {
	if cfg.Bool("fail", false) {
		return errors.New("connection refused")
	}
	return nil
}

type readCloser struct{ io.Reader }

func (readCloser) Close() error { return nil }

func (t *testSource) Backup(ctx context.Context, cfg core.Config, log *slog.Logger) (*source.Stream, error) {
	payload := cfg.StringOr("payload", "data")
	log.Info("test dump", "bytes", len(payload))
	return &source.Stream{Reader: readCloser{bytes.NewReader([]byte(payload))}, Extension: "dump", Size: int64(len(payload))}, nil
}

type testDestination struct{}

type testBucket struct {
	mu    sync.Mutex
	files map[string][]byte
}

var buckets sync.Map

func bucketFor(name string) *testBucket {
	v, _ := buckets.LoadOrStore(name, &testBucket{files: map[string][]byte{}})
	return v.(*testBucket)
}

func (t *testDestination) Spec() core.DriverSpec {
	return core.DriverSpec{
		Kind:     "testdest",
		Label:    "Test destination",
		Category: "test",
		Fields: []core.Field{
			{Name: "bucket", Label: "Bucket", Type: core.FieldString, Required: true},
			{Name: "secret_key", Label: "Secret key", Type: core.FieldSecret, Secret: true},
		},
		Capabilities: []string{core.CapTest, core.CapBrowse},
	}
}

func (t *testDestination) Validate(cfg core.Config) error {
	if cfg.String("bucket") == "" {
		return errors.New("bucket is required")
	}
	return nil
}

func (t *testDestination) Open(ctx context.Context, cfg core.Config, log *slog.Logger) (dest.Client, error) {
	return &testClient{bucket: bucketFor(cfg.String("bucket"))}, nil
}

type testClient struct{ bucket *testBucket }

func (c *testClient) Put(ctx context.Context, path string, r io.Reader, size int64) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	c.bucket.mu.Lock()
	defer c.bucket.mu.Unlock()
	c.bucket.files[path] = data
	return nil
}

func (c *testClient) Get(ctx context.Context, path string) (io.ReadCloser, error) {
	c.bucket.mu.Lock()
	defer c.bucket.mu.Unlock()
	data, ok := c.bucket.files[path]
	if !ok {
		return nil, fmt.Errorf("not found: %s", path)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (c *testClient) Delete(ctx context.Context, path string) error {
	c.bucket.mu.Lock()
	defer c.bucket.mu.Unlock()
	delete(c.bucket.files, path)
	return nil
}

func (c *testClient) List(ctx context.Context, prefix string) ([]dest.Object, error) {
	c.bucket.mu.Lock()
	defer c.bucket.mu.Unlock()
	out := []dest.Object{}
	for path, data := range c.bucket.files {
		out = append(out, dest.Object{Path: path, Size: int64(len(data)), ModTime: time.Now().UTC()})
	}
	return out, nil
}

func (c *testClient) Stat(ctx context.Context, path string) (*dest.Object, error) {
	c.bucket.mu.Lock()
	defer c.bucket.mu.Unlock()
	data, ok := c.bucket.files[path]
	if !ok {
		return nil, fmt.Errorf("not found: %s", path)
	}
	return &dest.Object{Path: path, Size: int64(len(data))}, nil
}

func (c *testClient) Test(ctx context.Context) error { return nil }

func (c *testClient) Close() error { return nil }

type testNotifier struct{}

func (t *testNotifier) Spec() core.DriverSpec {
	return core.DriverSpec{
		Kind:     "testnotify",
		Label:    "Test notifier",
		Category: "test",
		Fields:   []core.Field{{Name: "token", Label: "Token", Type: core.FieldSecret, Secret: true}},
	}
}

func (t *testNotifier) Validate(core.Config) error { return nil }

func (t *testNotifier) Send(ctx context.Context, cfg core.Config, ev notify.Event, log *slog.Logger) error {
	return nil
}

type testRemoteSource struct{}

func (t *testRemoteSource) Spec() core.DriverSpec {
	return core.DriverSpec{
		Kind:     "testremote",
		Label:    "Test remote source",
		Category: "test",
		Fields: []core.Field{
			{Name: "command", Label: "Command", Type: core.FieldString, Required: true},
			{Name: "ssh_password", Label: "SSH password", Type: core.FieldSecret, Secret: true, LocalOnly: true},
		},
		Capabilities: []string{core.CapTest, core.CapRemote},
	}
}

func (t *testRemoteSource) Validate(cfg core.Config) error {
	if cfg.String("command") == "" {
		return errors.New("command is required")
	}
	return nil
}

func (t *testRemoteSource) Test(ctx context.Context, cfg core.Config, log *slog.Logger) error {
	if cfg.Host() == nil && cfg.Bool("require_host", false) {
		return errors.New("no host attached")
	}
	return nil
}

func (t *testRemoteSource) Backup(ctx context.Context, cfg core.Config, log *slog.Logger) (*source.Stream, error) {
	payload := cfg.StringOr("command", "data")
	return &source.Stream{Reader: readCloser{bytes.NewReader([]byte(payload))}, Extension: "dump", Size: int64(len(payload))}, nil
}
