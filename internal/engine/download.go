package engine

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/arthurr0/backvault/internal/core"
)

type DownloadOptions struct {
	Raw        bool
	Passphrase string
}

type artifactReader struct {
	io.Reader
	closers []io.Closer
}

func (a *artifactReader) Close() error {
	var first error
	for i := len(a.closers) - 1; i >= 0; i-- {
		if err := a.closers[i].Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (e *Engine) OpenArtifact(ctx context.Context, artifact core.Artifact, opts DownloadOptions, log *slog.Logger) (io.ReadCloser, string, error) {
	if log == nil {
		log = e.log
	}
	d, err := e.store.Destinations.Get(ctx, artifact.DestinationID)
	if err != nil {
		return nil, "", fmt.Errorf("load destination: %w", err)
	}
	client, err := e.OpenDestination(ctx, d, log)
	if err != nil {
		return nil, "", err
	}
	body, err := client.Get(ctx, artifact.Path)
	if err != nil {
		_ = client.Close()
		return nil, "", fmt.Errorf("read %s from %s: %w", artifact.Path, d.Name, err)
	}

	out := &artifactReader{Reader: body, closers: []io.Closer{client, body}}
	if opts.Raw {
		return out, artifact.Filename, nil
	}

	packOpts := PackOptions{Compression: artifact.Compression, Encryption: artifact.Encryption}
	if packOpts.passthrough() {
		return out, artifact.Filename, nil
	}
	passphrase := opts.Passphrase
	if passphrase == "" && artifact.Encryption == core.EncryptionAge {
		passphrase, err = e.passphraseForArtifact(ctx, artifact)
		if err != nil {
			_ = out.Close()
			return nil, "", err
		}
	}
	packOpts.Passphrase = passphrase

	reader, closer, err := newUnpackReader(body, packOpts)
	if err != nil {
		_ = out.Close()
		return nil, "", err
	}
	out.Reader = reader
	out.closers = append([]io.Closer{closer}, out.closers...)
	return out, unpackedName(artifact.Filename), nil
}

func (e *Engine) passphraseForArtifact(ctx context.Context, artifact core.Artifact) (string, error) {
	if artifact.JobID == "" {
		return "", fmt.Errorf("artifact is encrypted and no passphrase was provided")
	}
	job, err := e.store.Jobs.Get(ctx, artifact.JobID)
	if err != nil {
		return "", fmt.Errorf("artifact is encrypted and the job is gone: %w", err)
	}
	passphrase, err := e.jobPassphrase(job)
	if err != nil {
		return "", err
	}
	if passphrase == "" {
		return "", fmt.Errorf("artifact is encrypted and no passphrase was provided")
	}
	return passphrase, nil
}

func unpackedName(filename string) string {
	name := strings.TrimSuffix(filename, ".age")
	name = strings.TrimSuffix(name, ".zst")
	name = strings.TrimSuffix(name, ".gz")
	return name
}
