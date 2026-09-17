package secrets

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const KeySize = 32

func ParseKey(value string) ([]byte, error) {
	v := strings.TrimSpace(value)
	if v == "" {
		return nil, errors.New("master key is empty")
	}
	if raw, err := hex.DecodeString(v); err == nil && len(raw) == KeySize {
		return raw, nil
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if raw, err := enc.DecodeString(v); err == nil && len(raw) == KeySize {
			return raw, nil
		}
	}
	return nil, fmt.Errorf("master key must be %d bytes encoded as hex or base64", KeySize)
}

func GenerateKey() ([]byte, error) {
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate master key: %w", err)
	}
	return key, nil
}

func LoadKey(envValue, path string) ([]byte, error) {
	if strings.TrimSpace(envValue) != "" {
		key, err := ParseKey(envValue)
		if err != nil {
			return nil, fmt.Errorf("master key from environment: %w", err)
		}
		return key, nil
	}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		key, perr := ParseKey(string(data))
		if perr != nil {
			return nil, fmt.Errorf("master key file %s: %w", path, perr)
		}
		return key, nil
	case errors.Is(err, fs.ErrNotExist):
		key, gerr := GenerateKey()
		if gerr != nil {
			return nil, gerr
		}
		if err := writeKeyFile(path, key); err != nil {
			return nil, err
		}
		return key, nil
	default:
		return nil, fmt.Errorf("read master key file %s: %w", path, err)
	}
}

func writeKeyFile(path string, key []byte) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create master key directory %s: %w", dir, err)
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(hex.EncodeToString(key)+"\n"), 0o600); err != nil {
		return fmt.Errorf("write master key file %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("install master key file %s: %w", path, err)
	}
	return os.Chmod(path, 0o600)
}
