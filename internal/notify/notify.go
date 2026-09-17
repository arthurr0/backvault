package notify

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/arthurr0/backvault/internal/core"
)

type Severity string

const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

type Event struct {
	Type     string            `json:"type"`
	Severity Severity          `json:"severity"`
	Title    string            `json:"title"`
	Message  string            `json:"message"`
	Time     time.Time         `json:"time"`
	SiteName string            `json:"siteName"`
	BaseURL  string            `json:"baseUrl"`
	Job      *core.Job         `json:"job,omitempty"`
	Run      *core.Run         `json:"run,omitempty"`
	Fields   map[string]string `json:"fields,omitempty"`
	Link     string            `json:"link,omitempty"`
}

type Driver interface {
	Spec() core.DriverSpec
	Validate(cfg core.Config) error
	Send(ctx context.Context, cfg core.Config, ev Event, log *slog.Logger) error
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
		panic(fmt.Sprintf("notifier already registered: %s", kind))
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
