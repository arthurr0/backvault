package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/arthurr0/backvault/internal/core"
)

type HostTestResult struct {
	OK         bool     `json:"ok"`
	Message    string   `json:"message"`
	OS         string   `json:"os"`
	Tools      []string `json:"tools"`
	DurationMS int64    `json:"durationMs"`
}

func (c *Client) Hosts(ctx context.Context, query string) ([]core.Host, error) {
	path := "/hosts?limit=500"
	if q := strings.TrimSpace(query); q != "" {
		path += "&q=" + url.QueryEscape(q)
	}
	var out List[core.Host]
	if err := c.Do(ctx, "GET", path, nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c *Client) Host(ctx context.Context, ref string) (core.Host, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return core.Host{}, fmt.Errorf("a host name or id is required")
	}
	var host core.Host
	err := c.Do(ctx, "GET", "/hosts/"+url.PathEscape(ref), nil, &host)
	if err == nil {
		return host, nil
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusNotFound {
		return core.Host{}, err
	}
	hosts, listErr := c.Hosts(ctx, "")
	if listErr != nil {
		return core.Host{}, err
	}
	for _, h := range hosts {
		if strings.EqualFold(h.Name, ref) {
			return h, nil
		}
	}
	return core.Host{}, err
}

func (c *Client) TestHost(ctx context.Context, id string) (HostTestResult, error) {
	var result HostTestResult
	if err := c.Do(ctx, "POST", "/hosts/"+url.PathEscape(id)+"/test", map[string]any{}, &result); err != nil {
		return result, err
	}
	return result, nil
}
