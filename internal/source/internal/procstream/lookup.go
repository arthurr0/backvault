package procstream

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func Lookup(binaryPath string, candidates ...string) (string, error) {
	if len(candidates) == 0 {
		return "", fmt.Errorf("no binary candidates given")
	}
	binaryPath = strings.TrimSpace(binaryPath)
	if binaryPath != "" {
		info, err := os.Stat(binaryPath)
		if err != nil {
			return "", fmt.Errorf("binary path %q: %w", binaryPath, err)
		}
		if info.IsDir() {
			for _, name := range candidates {
				full := filepath.Join(binaryPath, name)
				if executable(full) {
					return full, nil
				}
			}
			return "", fmt.Errorf("none of %s found in %q", strings.Join(candidates, ", "), binaryPath)
		}
		base := filepath.Base(binaryPath)
		for _, name := range candidates {
			if base == name {
				if !executable(binaryPath) {
					return "", fmt.Errorf("binary path %q is not executable", binaryPath)
				}
				return binaryPath, nil
			}
		}
		dir := filepath.Dir(binaryPath)
		for _, name := range candidates {
			full := filepath.Join(dir, name)
			if executable(full) {
				return full, nil
			}
		}
		if executable(binaryPath) {
			return binaryPath, nil
		}
		return "", fmt.Errorf("binary path %q does not provide %s", binaryPath, strings.Join(candidates, ", "))
	}
	for _, name := range candidates {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	if len(candidates) == 1 {
		return "", fmt.Errorf("%s not found in PATH, set the binary path field or install it", candidates[0])
	}
	return "", fmt.Errorf("none of %s found in PATH, set the binary path field or install one of them", strings.Join(candidates, ", "))
}

func Available(binaryPath string, candidates ...string) bool {
	_, err := Lookup(binaryPath, candidates...)
	return err == nil
}

func executable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode().Perm()&0o111 != 0
}
