package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const DefaultTimeout = 15 * time.Second

func Client(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &http.Client{Timeout: timeout}
}

func PostJSON(ctx context.Context, client *http.Client, target string, payload any, headers map[string]string) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode request body: %w", err)
	}
	return Post(ctx, client, target, "application/json", body, headers)
}

func Post(ctx context.Context, client *http.Client, target, contentType string, body []byte, headers map[string]string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", "Backvault")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return Do(client, req)
}

func Do(client *http.Client, req *http.Request) error {
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", req.Method, Safe(req.URL), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil
	}
	detail, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	message := strings.TrimSpace(string(detail))
	if message == "" {
		return fmt.Errorf("%s %s: %s", req.Method, Safe(req.URL), resp.Status)
	}
	return fmt.Errorf("%s %s: %s: %s", req.Method, Safe(req.URL), resp.Status, message)
}

func Safe(u *url.URL) string {
	if u == nil {
		return ""
	}
	clone := *u
	clone.User = nil
	clone.RawQuery = ""
	if len(clone.Path) > 24 {
		clone.Path = clone.Path[:12] + "..."
	}
	return clone.Scheme + "://" + clone.Host + clone.Path
}
