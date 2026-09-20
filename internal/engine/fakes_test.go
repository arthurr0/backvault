package engine

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

var registerFakes sync.Once

func init() {
	registerFakes.Do(func() {
		source.Register(&fakeSource{})
		source.Register(&blockingSource{})
		source.Register(&gatedSource{})
		source.Register(&remoteFakeSource{})
		dest.Register(&memDestination{})
		notify.Register(&captureNotifier{})
	})
}

type fakeSource struct{}

func (f *fakeSource) Spec() core.DriverSpec {
	return core.DriverSpec{
		Kind:     "fake",
		Label:    "Fake source",
		Category: "test",
		Fields: []core.Field{
			{Name: "payload", Label: "Payload", Type: core.FieldString},
			{Name: "secret_token", Label: "Token", Type: core.FieldSecret, Secret: true},
		},
		Capabilities: []string{core.CapTest, core.CapRestore},
	}
}

func (f *fakeSource) Validate(cfg core.Config) error {
	if cfg.String("invalid") != "" {
		return errors.New("source is configured as invalid")
	}
	return nil
}

func (f *fakeSource) Test(ctx context.Context, cfg core.Config, log *slog.Logger) error {
	if cfg.Bool("fail_test", false) {
		return errors.New("test failure requested")
	}
	return nil
}

type fakeStream struct {
	io.Reader
	closeErr error
}

func (s *fakeStream) Close() error { return s.closeErr }

func (f *fakeSource) Backup(ctx context.Context, cfg core.Config, log *slog.Logger) (*source.Stream, error) {
	if cfg.Bool("fail_backup", false) {
		return nil, errors.New("backup failure requested")
	}
	payload := cfg.StringOr("payload", "hello backvault")
	log.Info("fake dump started", "bytes", len(payload))
	var closeErr error
	if cfg.Bool("fail_close", false) {
		closeErr = errors.New("dump process exited with code 1")
	}
	return &source.Stream{
		Reader:    &fakeStream{Reader: bytes.NewReader([]byte(payload)), closeErr: closeErr},
		Extension: cfg.StringOr("extension", "dump"),
		Size:      int64(len(payload)),
	}, nil
}

var restoredMu sync.Mutex
var restored = map[string][]byte{}

func (f *fakeSource) Restore(ctx context.Context, cfg core.Config, r io.Reader, opts source.RestoreOptions, log *slog.Logger) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	restoredMu.Lock()
	restored[cfg.String("name")] = data
	restoredMu.Unlock()
	return nil
}

func restoredPayload(name string) []byte {
	restoredMu.Lock()
	defer restoredMu.Unlock()
	return restored[name]
}

type blockingSource struct{}

func (b *blockingSource) Spec() core.DriverSpec {
	return core.DriverSpec{Kind: "blocking", Label: "Blocking source", Category: "test", Capabilities: []string{core.CapTest}}
}

func (b *blockingSource) Validate(core.Config) error { return nil }

func (b *blockingSource) Test(context.Context, core.Config, *slog.Logger) error { return nil }

type blockingReader struct {
	ctx context.Context
}

func (b *blockingReader) Read(p []byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (b *blockingReader) Close() error { return nil }

func (b *blockingSource) Backup(ctx context.Context, cfg core.Config, log *slog.Logger) (*source.Stream, error) {
	return &source.Stream{Reader: &blockingReader{ctx: ctx}, Extension: "dump"}, nil
}

type memStore struct {
	mu    sync.Mutex
	files map[string][]byte
	times map[string]time.Time
}

var memStores sync.Map

func storeFor(name string) *memStore {
	v, _ := memStores.LoadOrStore(name, &memStore{files: map[string][]byte{}, times: map[string]time.Time{}})
	return v.(*memStore)
}

type memDestination struct{}

func (m *memDestination) Spec() core.DriverSpec {
	return core.DriverSpec{
		Kind:     "memory",
		Label:    "In-memory destination",
		Category: "test",
		Fields: []core.Field{
			{Name: "bucket", Label: "Bucket", Type: core.FieldString, Required: true},
			{Name: "secret_key", Label: "Secret", Type: core.FieldSecret, Secret: true},
		},
		Capabilities: []string{core.CapTest, core.CapBrowse},
	}
}

func (m *memDestination) Validate(cfg core.Config) error {
	if cfg.String("bucket") == "" {
		return errors.New("bucket is required")
	}
	return nil
}

func (m *memDestination) Open(ctx context.Context, cfg core.Config, log *slog.Logger) (dest.Client, error) {
	if cfg.Bool("fail_open", false) {
		return nil, errors.New("destination is unavailable")
	}
	return &memClient{
		store:    storeFor(cfg.String("bucket")),
		failPut:  cfg.Bool("fail_put", false),
		shortPut: cfg.Bool("short_put", false),
	}, nil
}

type memClient struct {
	store    *memStore
	failPut  bool
	shortPut bool
}

func (c *memClient) Put(ctx context.Context, path string, r io.Reader, size int64) error {
	if c.failPut {
		return errors.New("upload rejected")
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if c.shortPut && len(data) > 1 {
		data = data[:len(data)-1]
	}
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	c.store.files[path] = data
	c.store.times[path] = time.Now().UTC()
	return nil
}

func (c *memClient) Get(ctx context.Context, path string) (io.ReadCloser, error) {
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	data, ok := c.store.files[path]
	if !ok {
		return nil, fmt.Errorf("object %s not found", path)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (c *memClient) Delete(ctx context.Context, path string) error {
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	delete(c.store.files, path)
	delete(c.store.times, path)
	return nil
}

func (c *memClient) List(ctx context.Context, prefix string) ([]dest.Object, error) {
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	out := []dest.Object{}
	for path, data := range c.store.files {
		if prefix != "" && len(path) >= len(prefix) && path[:len(prefix)] != prefix {
			continue
		}
		out = append(out, dest.Object{Path: path, Size: int64(len(data)), ModTime: c.store.times[path]})
	}
	return out, nil
}

func (c *memClient) Stat(ctx context.Context, path string) (*dest.Object, error) {
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	data, ok := c.store.files[path]
	if !ok {
		return nil, fmt.Errorf("object %s not found", path)
	}
	return &dest.Object{Path: path, Size: int64(len(data)), ModTime: c.store.times[path]}, nil
}

func (c *memClient) Test(ctx context.Context) error { return nil }

func (c *memClient) Close() error { return nil }

type captureNotifier struct{}

var (
	notifiedMu sync.Mutex
	notified   []notify.Event
)

func (n *captureNotifier) Spec() core.DriverSpec {
	return core.DriverSpec{
		Kind:     "capture",
		Label:    "Capture notifier",
		Category: "test",
		Fields:   []core.Field{{Name: "token", Label: "Token", Type: core.FieldSecret, Secret: true}},
	}
}

func (n *captureNotifier) Validate(core.Config) error { return nil }

func (n *captureNotifier) Send(ctx context.Context, cfg core.Config, ev notify.Event, log *slog.Logger) error {
	notifiedMu.Lock()
	notified = append(notified, ev)
	notifiedMu.Unlock()
	return nil
}

func capturedEvents() []notify.Event {
	notifiedMu.Lock()
	defer notifiedMu.Unlock()
	out := make([]notify.Event, len(notified))
	copy(out, notified)
	return out
}

func resetCapturedEvents() {
	notifiedMu.Lock()
	notified = nil
	notifiedMu.Unlock()
}

type gate struct {
	payload  string
	closeErr error
	ready    chan struct{}
	onClose  func()
	runID    string
}

var gates sync.Map

func newGate(name, payload string) *gate {
	g := &gate{payload: payload, ready: make(chan struct{})}
	gates.Store(name, g)
	return g
}

func gateFor(name string) *gate {
	v, ok := gates.Load(name)
	if !ok {
		return nil
	}
	return v.(*gate)
}

type gateStream struct {
	io.Reader
	g *gate
}

func (s *gateStream) Close() error {
	if s.g.onClose != nil {
		s.g.onClose()
	}
	return s.g.closeErr
}

type gatedSource struct{}

func (g *gatedSource) Spec() core.DriverSpec {
	return core.DriverSpec{
		Kind:         "gated",
		Label:        "Gated source",
		Category:     "test",
		Fields:       []core.Field{{Name: "gate", Label: "Gate", Type: core.FieldString, Required: true}},
		Capabilities: []string{core.CapTest},
	}
}

func (g *gatedSource) Validate(core.Config) error { return nil }

func (g *gatedSource) Test(context.Context, core.Config, *slog.Logger) error { return nil }

func (g *gatedSource) Backup(ctx context.Context, cfg core.Config, log *slog.Logger) (*source.Stream, error) {
	target := gateFor(cfg.String("gate"))
	if target == nil {
		return nil, errors.New("no gate registered")
	}
	select {
	case <-target.ready:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return &source.Stream{
		Reader:    &gateStream{Reader: bytes.NewReader([]byte(target.payload)), g: target},
		Extension: "dump",
		Size:      int64(len(target.payload)),
	}, nil
}

var (
	seenHostsMu sync.Mutex
	seenHosts   = map[string]string{}
)

const noHostSeen = "<none>"

func recordHost(stage string, cfg core.Config) {
	name := noHostSeen
	if h := cfg.Host(); h != nil {
		name = h.Name
	}
	seenHostsMu.Lock()
	seenHosts[stage] = name
	seenHostsMu.Unlock()
}

func seenHost(stage string) string {
	seenHostsMu.Lock()
	defer seenHostsMu.Unlock()
	name, ok := seenHosts[stage]
	if !ok {
		return ""
	}
	return name
}

func resetSeenHosts() {
	seenHostsMu.Lock()
	seenHosts = map[string]string{}
	seenHostsMu.Unlock()
}

type remoteFakeSource struct{}

func (r *remoteFakeSource) Spec() core.DriverSpec {
	return core.DriverSpec{
		Kind:         "remotefake",
		Label:        "Remote fake source",
		Category:     "test",
		Fields:       []core.Field{{Name: "payload", Label: "Payload", Type: core.FieldString}},
		Capabilities: []string{core.CapTest, core.CapRestore, core.CapRemote},
	}
}

func (r *remoteFakeSource) Validate(core.Config) error { return nil }

func (r *remoteFakeSource) Test(ctx context.Context, cfg core.Config, log *slog.Logger) error {
	recordHost("test", cfg)
	return nil
}

func (r *remoteFakeSource) Backup(ctx context.Context, cfg core.Config, log *slog.Logger) (*source.Stream, error) {
	recordHost("backup", cfg)
	payload := cfg.StringOr("payload", "remote payload")
	return &source.Stream{Reader: &fakeStream{Reader: bytes.NewReader([]byte(payload))}, Extension: "dump", Size: int64(len(payload))}, nil
}

func (r *remoteFakeSource) Restore(ctx context.Context, cfg core.Config, rd io.Reader, opts source.RestoreOptions, log *slog.Logger) error {
	recordHost("restore", cfg)
	_, err := io.Copy(io.Discard, rd)
	return err
}
