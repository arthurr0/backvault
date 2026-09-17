package procstream

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type TempDir struct {
	Path string
}

func NewTempDir(prefix string) (*TempDir, error) {
	dir, err := os.MkdirTemp("", "backvault-"+prefix+"-")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	return &TempDir{Path: dir}, nil
}

func (t *TempDir) File(name string) string {
	return filepath.Join(t.Path, name)
}

func (t *TempDir) Remove() {
	if t == nil || t.Path == "" {
		return
	}
	_ = os.RemoveAll(t.Path)
}

type fileStream struct {
	f       *os.File
	cleanup []func()
}

func FileStream(path string, cleanup ...func()) (io.ReadCloser, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		runCleanup(cleanup)
		return nil, 0, fmt.Errorf("open %s: %w", path, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		runCleanup(cleanup)
		return nil, 0, fmt.Errorf("stat %s: %w", path, err)
	}
	return &fileStream{f: f, cleanup: cleanup}, info.Size(), nil
}

func (s *fileStream) Read(b []byte) (int, error) { return s.f.Read(b) }

func (s *fileStream) Close() error {
	err := s.f.Close()
	runCleanup(s.cleanup)
	return err
}

func WriteTempFile(dir, pattern string, mode os.FileMode, content []byte) (string, error) {
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("chmod temp file: %w", err)
	}
	if _, err := f.Write(content); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("write temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("close temp file: %w", err)
	}
	return f.Name(), nil
}
