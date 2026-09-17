package dest

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/arthurr0/backvault/internal/core"
)

type Object struct {
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
	IsDir   bool      `json:"isDir"`
}

type Client interface {
	Put(ctx context.Context, path string, r io.Reader, size int64) error
	Get(ctx context.Context, path string) (io.ReadCloser, error)
	Delete(ctx context.Context, path string) error
	List(ctx context.Context, prefix string) ([]Object, error)
	Stat(ctx context.Context, path string) (*Object, error)
	Test(ctx context.Context) error
	Close() error
}

type Driver interface {
	Spec() core.DriverSpec
	Validate(cfg core.Config) error
	Open(ctx context.Context, cfg core.Config, log *slog.Logger) (Client, error)
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
		panic(fmt.Sprintf("destination driver already registered: %s", kind))
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
