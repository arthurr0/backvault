package source

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"sync"

	"github.com/arthurr0/backvault/internal/core"
)

type Stream struct {
	Reader    io.ReadCloser
	Extension string
	Size      int64
	Meta      map[string]string
}

type Driver interface {
	Spec() core.DriverSpec
	Validate(cfg core.Config) error
	Test(ctx context.Context, cfg core.Config, log *slog.Logger) error
	Backup(ctx context.Context, cfg core.Config, log *slog.Logger) (*Stream, error)
}

type RestoreOptions struct {
	Extension  string
	Size       int64
	Overwrite  bool
	TargetPath string
	Params     core.Config
}

type Restorer interface {
	Restore(ctx context.Context, cfg core.Config, r io.Reader, opts RestoreOptions, log *slog.Logger) error
}

var (
	mu       sync.RWMutex
	registry = map[string]Driver{}
)

func Register(d Driver) {
	mu.Lock()
	defer mu.Unlock()
	kind := d.Spec().Kind
	if _, exists := registry[kind]; exists {
		panic(fmt.Sprintf("source driver already registered: %s", kind))
	}
	registry[kind] = d
}

func Get(kind string) (Driver, bool) {
	mu.RLock()
	defer mu.RUnlock()
	d, ok := registry[kind]
	return d, ok
}

func All() []Driver {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Driver, 0, len(registry))
	for _, d := range registry {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Spec().Label < out[j].Spec().Label })
	return out
}

func Specs() []core.DriverSpec {
	drivers := All()
	out := make([]core.DriverSpec, 0, len(drivers))
	for _, d := range drivers {
		out = append(out, d.Spec())
	}
	return out
}
