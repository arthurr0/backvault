package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/arthurr0/backvault/internal/client"
	"github.com/arthurr0/backvault/internal/core"
)

func newPushCommand() *cobra.Command {
	var (
		job        string
		file       string
		name       string
		packed     bool
		withSHA    bool
		retries    int
		retryDelay time.Duration
	)
	cmd := &cobra.Command{
		Use:   "push",
		Short: "Stream a file or stdin to the ingest API",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(job) == "" {
				return errors.New("--job is required")
			}
			c, err := apiClient()
			if err != nil {
				return err
			}

			source := file
			cleanup := func() {}
			if strings.TrimSpace(source) == "" || source == "-" {
				tmp, err := spoolStdin()
				if err != nil {
					return err
				}
				source = tmp
				cleanup = func() { _ = os.Remove(tmp) }
				if name == "" {
					name = "stdin.bin"
				}
			}
			defer cleanup()

			if name == "" {
				name = filepath.Base(source)
			}

			info, err := os.Stat(source)
			if err != nil {
				return fmt.Errorf("stat %s: %w", source, err)
			}

			checksum := ""
			if withSHA {
				checksum, err = fileSHA256(source)
				if err != nil {
					return err
				}
			}

			path := "/ingest/" + job
			if packed {
				path += "?packed=1"
			}

			var lastErr error
			for attempt := 1; attempt <= retries+1; attempt++ {
				if attempt > 1 {
					wait := retryDelay * time.Duration(1<<uint(attempt-2))
					if wait > 5*time.Minute {
						wait = 5 * time.Minute
					}
					fmt.Fprintf(os.Stderr, "retrying in %s (attempt %d)\n", wait, attempt)
					select {
					case <-cmd.Context().Done():
						return cmd.Context().Err()
					case <-time.After(wait):
					}
				}
				runID, err := pushOnce(cmd.Context(), c, path, source, name, checksum, info.Size())
				if err == nil {
					fmt.Println(runID)
					return nil
				}
				lastErr = err
				var apiErr *client.APIError
				if errors.As(err, &apiErr) && apiErr.Status < 500 && apiErr.Status != 423 {
					return err
				}
			}
			return lastErr
		},
	}
	cmd.Flags().StringVar(&job, "job", "", "job slug to push to")
	cmd.Flags().StringVar(&file, "file", "", "file to upload, or - for stdin")
	cmd.Flags().StringVar(&name, "name", "", "original filename reported to the server")
	cmd.Flags().BoolVar(&packed, "packed", false, "the payload is already compressed or encrypted")
	cmd.Flags().BoolVar(&withSHA, "sha256", false, "compute and send the sha256 of the payload")
	cmd.Flags().IntVar(&retries, "retries", 3, "number of retries")
	cmd.Flags().DurationVar(&retryDelay, "retry-delay", 5*time.Second, "initial retry delay")
	return cmd
}

func pushOnce(ctx context.Context, c *client.Client, path, source, name, checksum string, size int64) (string, error) {
	f, err := os.Open(source)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", source, err)
	}
	defer f.Close()
	headers := map[string]string{
		"X-Backvault-Filename": name,
		"X-Backvault-Sha256":   checksum,
	}
	var result struct {
		Run       core.Run        `json:"run"`
		Artifacts []core.Artifact `json:"artifacts"`
	}
	if err := c.Stream(ctx, "POST", path, f, headers, size, &result); err != nil {
		return "", err
	}
	return result.Run.ID, nil
}

func spoolStdin() (string, error) {
	tmp, err := os.CreateTemp("", "backvault-push-*")
	if err != nil {
		return "", fmt.Errorf("create temporary file: %w", err)
	}
	defer tmp.Close()
	if _, err := io.Copy(tmp, os.Stdin); err != nil {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("read stdin: %w", err)
	}
	return tmp.Name(), nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
