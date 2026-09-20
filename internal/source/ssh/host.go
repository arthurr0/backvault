package ssh

import (
	"context"
	"io"
	"log/slog"
	"strings"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/source"
	"github.com/arthurr0/backvault/internal/source/internal/remoteexec"
)

func backupOnHost(ctx context.Context, cfg core.Config, command string, log *slog.Logger) (*source.Stream, error) {
	r, err := remoteexec.Open(ctx, cfg, log)
	if err != nil {
		return nil, err
	}
	log.Info("streaming command on the host", "host", r.HostLabel(), "command", command)
	reader, err := r.Start(ctx, remoteexec.Command{Script: command, Label: "command"})
	if err != nil {
		r.Close()
		return nil, err
	}
	return &source.Stream{
		Reader:    reader,
		Extension: strings.TrimPrefix(cfg.StringOr("extension", "tar"), "."),
		Meta:      map[string]string{"host": r.HostLabel(), "command": command},
	}, nil
}

func restoreOnHost(ctx context.Context, cfg core.Config, command string, src io.Reader, log *slog.Logger) error {
	r, err := remoteexec.Open(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer r.Close()
	log.Info("running restore command on the host", "host", r.HostLabel(), "command", command)
	return r.Run(ctx, remoteexec.Command{Script: command, Stdin: src, Label: "restore"})
}
