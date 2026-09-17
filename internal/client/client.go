package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

type APIError struct {
	Status  int
	Code    string
	Message string
	Fields  map[string]string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("request failed with status %d", e.Status)
	}
	if len(e.Fields) > 0 {
		parts := make([]string, 0, len(e.Fields))
		for k, v := range e.Fields {
			parts = append(parts, k+": "+v)
		}
		return fmt.Sprintf("%s (%s)", e.Message, strings.Join(parts, ", "))
	}
	return e.Message
}

func New() (*Client, error) {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("BACKVAULT_URL")), "/")
	if base == "" {
		base = "http://localhost:8080"
	}
	if _, err := url.Parse(base); err != nil {
		return nil, fmt.Errorf("invalid BACKVAULT_URL %q: %w", base, err)
	}
	return &Client{
		BaseURL: base,
		Token:   strings.TrimSpace(os.Getenv("BACKVAULT_TOKEN")),
		HTTP:    &http.Client{Timeout: 5 * time.Minute},
	}, nil
}

func (c *Client) endpoint(path string) string {
	if strings.HasPrefix(path, "/api/") {
		return c.BaseURL + path
	}
	return c.BaseURL + "/api/v1" + path
}

func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint(path), body)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func (c *Client) Do(ctx context.Context, method, path string, payload any, out any) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := c.newRequest(ctx, method, path, body)
	if err != nil {
		return err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	return decodeResponse(resp, out)
}

func (c *Client) Raw(ctx context.Context, method, path string) (*http.Response, error) {
	req, err := c.newRequest(ctx, method, path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		return nil, readAPIError(resp)
	}
	return resp, nil
}

func (c *Client) Stream(ctx context.Context, method, path string, body io.Reader, headers map[string]string, contentLength int64, out any) error {
	req, err := c.newRequest(ctx, method, path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	if contentLength >= 0 {
		req.ContentLength = contentLength
	}
	for k, v := range headers {
		if v != "" {
			req.Header.Set(k, v)
		}
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	return decodeResponse(resp, out)
}

func decodeResponse(resp *http.Response, out any) error {
	if resp.StatusCode >= 400 {
		return readAPIError(resp)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil && err != io.EOF {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func readAPIError(resp *http.Response) error {
	var envelope struct {
		Error struct {
			Code    string            `json:"code"`
			Message string            `json:"message"`
			Fields  map[string]string `json:"fields"`
		} `json:"error"`
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err := json.Unmarshal(body, &envelope); err == nil && envelope.Error.Message != "" {
		return &APIError{Status: resp.StatusCode, Code: envelope.Error.Code, Message: envelope.Error.Message, Fields: envelope.Error.Fields}
	}
	message := strings.TrimSpace(string(body))
	if len(message) > 500 {
		message = message[:500]
	}
	return &APIError{Status: resp.StatusCode, Message: message}
}

type List[T any] struct {
	Items []T `json:"items"`
	Total int `json:"total"`
}
