package engine

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/dest"
	"github.com/arthurr0/backvault/internal/notify"
	"github.com/arthurr0/backvault/internal/source"
)

func (e *Engine) SourceConfig(src core.Source) (source.Driver, core.Config, error) {
	driver, ok := source.Get(src.Kind)
	if !ok {
		return nil, nil, fmt.Errorf("unknown source driver %q", src.Kind)
	}
	cfg, err := e.secrets.DecryptConfig(src.Config, driver.Spec().SecretFields())
	if err != nil {
		return nil, nil, err
	}
	return driver, cfg, nil
}

func (e *Engine) DestinationConfig(d core.Destination) (dest.Driver, core.Config, error) {
	driver, ok := dest.Get(d.Kind)
	if !ok {
		return nil, nil, fmt.Errorf("unknown destination driver %q", d.Kind)
	}
	cfg, err := e.secrets.DecryptConfig(d.Config, driver.Spec().SecretFields())
	if err != nil {
		return nil, nil, err
	}
	return driver, cfg, nil
}

func (e *Engine) NotifierConfig(c core.NotificationChannel) (notify.Driver, core.Config, error) {
	driver, ok := notify.Get(c.Kind)
	if !ok {
		return nil, nil, fmt.Errorf("unknown notifier %q", c.Kind)
	}
	cfg, err := e.secrets.DecryptConfig(c.Config, driver.Spec().SecretFields())
	if err != nil {
		return nil, nil, err
	}
	return driver, cfg, nil
}

func (e *Engine) OpenDestination(ctx context.Context, d core.Destination, log *slog.Logger) (dest.Client, error) {
	driver, cfg, err := e.DestinationConfig(d)
	if err != nil {
		return nil, err
	}
	client, err := driver.Open(ctx, cfg, log)
	if err != nil {
		return nil, fmt.Errorf("open destination %s: %w", d.Name, err)
	}
	return client, nil
}

type TestResult struct {
	OK         bool   `json:"ok"`
	Message    string `json:"message"`
	DurationMS int64  `json:"durationMs"`
}

func (e *Engine) TestSourceConfig(ctx context.Context, kind string, cfg core.Config, hostID string, log *slog.Logger) TestResult {
	driver, ok := source.Get(kind)
	if !ok {
		return TestResult{Message: fmt.Sprintf("unknown source driver %q", kind)}
	}
	plain, err := e.secrets.DecryptConfig(cfg, driver.Spec().SecretFields())
	if err != nil {
		return TestResult{Message: err.Error()}
	}
	if err := driver.Validate(plain); err != nil {
		return TestResult{Message: err.Error()}
	}
	plain, err = e.attachHost(ctx, driver.Spec(), hostID, plain)
	if err != nil {
		return TestResult{Message: err.Error()}
	}
	return timedTest(ctx, log, func(c context.Context, l *slog.Logger) error {
		return driver.Test(c, plain, l)
	})
}

func (e *Engine) TestSource(ctx context.Context, src core.Source, log *slog.Logger) TestResult {
	return e.TestSourceConfig(ctx, src.Kind, src.Config, src.HostID, log)
}

func (e *Engine) TestDestinationConfig(ctx context.Context, kind string, cfg core.Config, log *slog.Logger) TestResult {
	driver, ok := dest.Get(kind)
	if !ok {
		return TestResult{Message: fmt.Sprintf("unknown destination driver %q", kind)}
	}
	plain, err := e.secrets.DecryptConfig(cfg, driver.Spec().SecretFields())
	if err != nil {
		return TestResult{Message: err.Error()}
	}
	if err := driver.Validate(plain); err != nil {
		return TestResult{Message: err.Error()}
	}
	return timedTest(ctx, log, func(c context.Context, l *slog.Logger) error {
		client, err := driver.Open(c, plain, l)
		if err != nil {
			return err
		}
		defer client.Close()
		return client.Test(c)
	})
}

func (e *Engine) TestChannelConfig(ctx context.Context, kind string, cfg core.Config, log *slog.Logger) TestResult {
	driver, ok := notify.Get(kind)
	if !ok {
		return TestResult{Message: fmt.Sprintf("unknown notifier %q", kind)}
	}
	plain, err := e.secrets.DecryptConfig(cfg, driver.Spec().SecretFields())
	if err != nil {
		return TestResult{Message: err.Error()}
	}
	if err := driver.Validate(plain); err != nil {
		return TestResult{Message: err.Error()}
	}
	settings := e.settings(ctx)
	ev := notify.Event{
		Type:     core.EventRunSuccess,
		Severity: notify.SeverityInfo,
		Title:    "Backvault test notification",
		Message:  "This is a test notification from Backvault. Delivery is working.",
		Time:     time.Now().UTC(),
		SiteName: settings.SiteName,
		BaseURL:  settings.BaseURL,
		Link:     settings.BaseURL,
	}
	return timedTest(ctx, log, func(c context.Context, l *slog.Logger) error {
		return driver.Send(c, plain, ev, l)
	})
}

func (e *Engine) BrowseDestination(ctx context.Context, d core.Destination, prefix string, log *slog.Logger) ([]dest.Object, error) {
	client, err := e.OpenDestination(ctx, d, log)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	items, err := client.List(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("list destination %s: %w", d.Name, err)
	}
	if items == nil {
		items = []dest.Object{}
	}
	return items, nil
}

func timedTest(ctx context.Context, log *slog.Logger, fn func(context.Context, *slog.Logger) error) TestResult {
	if log == nil {
		log = slog.Default()
	}
	ctx, cancel := context.WithTimeout(ctx, driverTestPeriod)
	defer cancel()
	started := time.Now()
	err := fn(ctx, log)
	res := TestResult{DurationMS: time.Since(started).Milliseconds()}
	if err != nil {
		res.Message = err.Error()
		return res
	}
	res.OK = true
	res.Message = "connection successful"
	return res
}
