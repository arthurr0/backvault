package engine

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/remote"
	"github.com/arthurr0/backvault/internal/source"
)

type HostTestResult struct {
	OK         bool     `json:"ok"`
	Message    string   `json:"message"`
	OS         string   `json:"os,omitempty"`
	Tools      []string `json:"tools"`
	DurationMS int64    `json:"durationMs"`
}

func (e *Engine) LoadHost(ctx context.Context, id string) (core.Host, error) {
	stored, err := e.store.Hosts.Get(ctx, id)
	if err != nil {
		return core.Host{}, fmt.Errorf("load host: %w", err)
	}
	host, err := e.secrets.DecryptHost(stored)
	if err != nil {
		return core.Host{}, fmt.Errorf("load host %s: %w", stored.Name, err)
	}
	return host, nil
}

func (e *Engine) attachHost(ctx context.Context, spec core.DriverSpec, hostID string, cfg core.Config) (core.Config, error) {
	if strings.TrimSpace(hostID) == "" {
		return cfg, nil
	}
	if !spec.Has(core.CapRemote) {
		return cfg, fmt.Errorf("source driver %s cannot run on a host", spec.Kind)
	}
	host, err := e.LoadHost(ctx, hostID)
	if err != nil {
		return cfg, err
	}
	return cfg.WithHost(&host), nil
}

func (e *Engine) SourceRuntime(ctx context.Context, src core.Source) (source.Driver, core.Config, error) {
	driver, cfg, err := e.SourceConfig(src)
	if err != nil {
		return nil, nil, err
	}
	cfg, err = e.attachHost(ctx, driver.Spec(), src.HostID, cfg)
	if err != nil {
		return nil, nil, err
	}
	return driver, cfg, nil
}

func hostNameOf(cfg core.Config) string {
	if h := cfg.Host(); h != nil {
		return h.Name
	}
	return ""
}

func (e *Engine) TestHost(ctx context.Context, host core.Host, log *slog.Logger) HostTestResult {
	if log == nil {
		log = slog.Default()
	}
	res := HostTestResult{Tools: []string{}}
	started := time.Now()
	if err := remote.Validate(host); err != nil {
		res.Message = err.Error()
		res.DurationMS = time.Since(started).Milliseconds()
		return res
	}
	ctx, cancel := context.WithTimeout(ctx, driverTestPeriod)
	defer cancel()
	client, err := remote.Dial(ctx, host, log)
	if err != nil {
		res.Message = remote.ScrubText(host, err.Error())
		res.DurationMS = time.Since(started).Milliseconds()
		return res
	}
	defer client.Close()
	probe, err := client.Probe(ctx)
	if err != nil {
		res.Message = remote.ScrubText(host, err.Error())
		res.DurationMS = time.Since(started).Milliseconds()
		return res
	}
	res.OK = true
	res.OS = probe.OS
	if probe.Tools != nil {
		res.Tools = probe.Tools
	}
	res.Message = "connection successful"
	res.DurationMS = time.Since(started).Milliseconds()
	return res
}
