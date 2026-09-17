package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
)

type countingWriter struct {
	w io.Writer
	n int64
	h hash.Hash
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	if c.h != nil && n > 0 {
		c.h.Write(p[:n])
	}
	return n, err
}

type countingReader struct {
	r   io.Reader
	n   int64
	ctx context.Context
}

func (c *countingReader) Read(p []byte) (int, error) {
	if c.ctx != nil {
		if err := c.ctx.Err(); err != nil {
			return 0, err
		}
	}
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

type spool struct {
	dir         string
	path        string
	Filename    string
	RawBytes    int64
	PackedBytes int64
	SHA256      string
}

func newSpool(workDir, runID, filename string) (*spool, error) {
	dir := filepath.Join(workDir, runID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create spool directory: %w", err)
	}
	return &spool{dir: dir, path: filepath.Join(dir, filename), Filename: filename}, nil
}

func (s *spool) Path() string { return s.path }

func (s *spool) Fill(ctx context.Context, src io.Reader, opts PackOptions) error {
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create spool file: %w", err)
	}
	defer f.Close()

	sink := &countingWriter{w: f, h: sha256.New()}
	packed, closer, err := newPackWriter(sink, opts)
	if err != nil {
		return err
	}
	reader := &countingReader{r: src, ctx: ctx}
	if _, err := io.Copy(packed, reader); err != nil {
		_ = closer.Close()
		return fmt.Errorf("copy stream into spool: %w", err)
	}
	if err := closer.Close(); err != nil {
		return fmt.Errorf("finish pack chain: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync spool file: %w", err)
	}
	s.RawBytes = reader.n
	s.PackedBytes = sink.n
	s.SHA256 = hex.EncodeToString(sink.h.Sum(nil))
	return nil
}

func (s *spool) Open() (*os.File, error) {
	f, err := os.Open(s.path)
	if err != nil {
		return nil, fmt.Errorf("open spool file: %w", err)
	}
	return f, nil
}

func (s *spool) Cleanup() error {
	if s.dir == "" {
		return nil
	}
	if err := os.RemoveAll(s.dir); err != nil {
		return fmt.Errorf("remove spool directory: %w", err)
	}
	return nil
}
