package webdav

import (
	"context"
	"crypto/tls"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/dest"
)

type Driver struct{}

func New() *Driver { return &Driver{} }

func init() { dest.Register(New()) }

func (d *Driver) Spec() core.DriverSpec {
	return core.DriverSpec{
		Kind:        "webdav",
		Label:       "WebDAV",
		Description: "Any WebDAV share, such as Nextcloud, ownCloud or the WebDAV endpoint of a Hetzner Storage Box.",
		Icon:        "cloud",
		Category:    "Remote",
		Capabilities: []string{
			core.CapTest,
			core.CapBrowse,
		},
		Fields: []core.Field{
			{
				Name: "url", Label: "Server URL", Type: core.FieldString, Required: true, Group: "Connection",
				Placeholder: "https://cloud.example.com/remote.php/dav/files/alice",
				Help: "Full URL of the WebDAV endpoint. Nextcloud uses /remote.php/dav/files/<user>. " +
					"Hetzner Storage Box uses https://<user>.your-storagebox.de.",
			},
			{
				Name: "user", Label: "User", Type: core.FieldString, Group: "Connection",
				Placeholder: "alice",
			},
			{
				Name: "password", Label: "Password", Type: core.FieldSecret, Secret: true, Group: "Connection",
				Help: "Use an app password where the provider offers one.",
			},
			{
				Name: "base_path", Label: "Base path", Type: core.FieldPath, Group: "Storage",
				Placeholder: "backups/backvault",
				Help:        "Folder below the server URL that holds the artifacts. It is created if missing.",
			},
			{
				Name: "skip_tls_verify", Label: "Skip certificate verification", Type: core.FieldBool,
				Default: false, Group: "Advanced", Advanced: true,
				Help: "Only for self-signed certificates on a trusted network.",
			},
			{
				Name: "timeout", Label: "Request timeout (seconds)", Type: core.FieldInt, Default: 60,
				Group: "Advanced", Advanced: true,
				Help: "Applies to metadata requests. Uploads and downloads are not limited by it.",
			},
		},
	}
}

func (d *Driver) Validate(cfg core.Config) error {
	if err := core.ValidateRequired(d.Spec(), cfg); err != nil {
		return err
	}
	u, err := url.Parse(strings.TrimSpace(cfg.String("url")))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("server url must be a full url such as https://cloud.example.com/remote.php/dav/files/alice")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("server url must use http or https")
	}
	return nil
}

func (d *Driver) Open(ctx context.Context, cfg core.Config, log *slog.Logger) (dest.Client, error) {
	if err := d.Validate(cfg); err != nil {
		return nil, err
	}
	base, err := url.Parse(strings.TrimSuffix(strings.TrimSpace(cfg.String("url")), "/"))
	if err != nil {
		return nil, fmt.Errorf("parse server url: %w", err)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.Bool("skip_tls_verify", false) {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		log.Warn("tls certificate verification is disabled for this webdav destination")
	}
	timeout := time.Duration(cfg.Int("timeout", 60)) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &client{
		base:     base,
		basePath: strings.Trim(cfg.StringOr("base_path", ""), "/"),
		user:     cfg.String("user"),
		password: cfg.String("password"),
		http:     &http.Client{Transport: transport},
		timeout:  timeout,
		log:      log,
	}, nil
}

type client struct {
	base     *url.URL
	basePath string
	user     string
	password string
	http     *http.Client
	timeout  time.Duration
	log      *slog.Logger
}

func (c *client) remotePath(p string) (string, error) {
	clean := path.Clean(strings.TrimPrefix(strings.ReplaceAll(p, "\\", "/"), "/"))
	if clean == "." || clean == "/" {
		clean = ""
	}
	if clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return "", fmt.Errorf("path %q escapes the base path", p)
	}
	switch {
	case c.basePath == "":
		return clean, nil
	case clean == "":
		return c.basePath, nil
	default:
		return c.basePath + "/" + clean, nil
	}
}

func (c *client) urlFor(rel string) string {
	u := *c.base
	segments := strings.Split(rel, "/")
	escaped := make([]string, 0, len(segments))
	for _, s := range segments {
		if s == "" {
			continue
		}
		escaped = append(escaped, url.PathEscape(s))
	}
	u.Path = strings.TrimSuffix(u.Path, "/")
	if len(escaped) > 0 {
		u.Path += "/" + strings.Join(escaped, "/")
	}
	u.RawPath = ""
	return u.String()
}

func (c *client) request(ctx context.Context, method, target string, body io.Reader, size int64) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, fmt.Errorf("build %s request: %w", method, err)
	}
	if size >= 0 {
		req.ContentLength = size
	}
	if c.user != "" || c.password != "" {
		req.SetBasicAuth(c.user, c.password)
	}
	return req, nil
}

func (c *client) do(req *http.Request, accepted ...int) (*http.Response, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", req.Method, redact(req.URL), err)
	}
	for _, code := range accepted {
		if resp.StatusCode == code {
			return resp, nil
		}
	}
	detail, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	resp.Body.Close()
	message := strings.TrimSpace(string(detail))
	if len(message) > 300 {
		message = message[:300]
	}
	if message == "" {
		return nil, fmt.Errorf("%s %s: %s", req.Method, redact(req.URL), resp.Status)
	}
	return nil, fmt.Errorf("%s %s: %s: %s", req.Method, redact(req.URL), resp.Status, message)
}

func (c *client) mkdirAll(ctx context.Context, rel string) error {
	if rel == "" {
		return nil
	}
	segments := strings.Split(rel, "/")
	current := ""
	for _, s := range segments {
		if s == "" {
			continue
		}
		if current == "" {
			current = s
		} else {
			current += "/" + s
		}
		req, err := c.request(ctx, "MKCOL", c.urlFor(current), nil, 0)
		if err != nil {
			return err
		}
		resp, err := c.do(req, http.StatusCreated, http.StatusMethodNotAllowed, http.StatusOK, http.StatusNoContent, http.StatusConflict)
		if err != nil {
			return fmt.Errorf("create remote directory %s: %w", current, err)
		}
		status := resp.StatusCode
		resp.Body.Close()
		if status == http.StatusConflict {
			return fmt.Errorf("create remote directory %s: parent does not exist", current)
		}
	}
	return nil
}

func (c *client) Put(ctx context.Context, p string, r io.Reader, size int64) error {
	rel, err := c.remotePath(p)
	if err != nil {
		return err
	}
	if dir := path.Dir(rel); dir != "." && dir != "" {
		if err := c.mkdirAll(ctx, dir); err != nil {
			return err
		}
	}
	partial := rel + ".partial"
	req, err := c.request(ctx, http.MethodPut, c.urlFor(partial), r, size)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := c.do(req, http.StatusCreated, http.StatusOK, http.StatusNoContent)
	if err != nil {
		_ = c.deletePath(ctx, partial)
		return fmt.Errorf("upload %s: %w", p, err)
	}
	resp.Body.Close()

	move, err := c.request(ctx, "MOVE", c.urlFor(partial), nil, 0)
	if err != nil {
		return err
	}
	move.Header.Set("Destination", c.urlFor(rel))
	move.Header.Set("Overwrite", "T")
	moveResp, err := c.do(move, http.StatusCreated, http.StatusNoContent, http.StatusOK)
	if err != nil {
		_ = c.deletePath(ctx, partial)
		return fmt.Errorf("move %s into place: %w", p, err)
	}
	moveResp.Body.Close()
	return nil
}

func (c *client) Get(ctx context.Context, p string) (io.ReadCloser, error) {
	rel, err := c.remotePath(p)
	if err != nil {
		return nil, err
	}
	req, err := c.request(ctx, http.MethodGet, c.urlFor(rel), nil, -1)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req, http.StatusOK, http.StatusPartialContent)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", p, err)
	}
	return resp.Body, nil
}

func (c *client) Delete(ctx context.Context, p string) error {
	rel, err := c.remotePath(p)
	if err != nil {
		return err
	}
	return c.deletePath(ctx, rel)
}

func (c *client) deletePath(ctx context.Context, rel string) error {
	reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := c.request(reqCtx, http.MethodDelete, c.urlFor(rel), nil, 0)
	if err != nil {
		return err
	}
	resp, err := c.do(req, http.StatusNoContent, http.StatusOK, http.StatusAccepted, http.StatusNotFound)
	if err != nil {
		return fmt.Errorf("delete %s: %w", rel, err)
	}
	resp.Body.Close()
	return nil
}

func (c *client) List(ctx context.Context, prefix string) ([]dest.Object, error) {
	root, err := c.remotePath(prefix)
	if err != nil {
		return nil, err
	}
	out := []dest.Object{}
	queue := []string{root}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		entries, err := c.propfind(ctx, current)
		if err != nil {
			if current == root {
				return out, nil
			}
			c.log.Warn("skipping unreadable remote directory", "path", current, "error", err.Error())
			continue
		}
		for _, e := range entries {
			if e.rel == current {
				continue
			}
			rel := c.relative(e.rel)
			if rel == "" {
				continue
			}
			if strings.HasSuffix(rel, ".partial") {
				continue
			}
			out = append(out, dest.Object{Path: rel, Size: e.size, ModTime: e.modTime, IsDir: e.dir})
			if e.dir {
				queue = append(queue, e.rel)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func (c *client) Stat(ctx context.Context, p string) (*dest.Object, error) {
	rel, err := c.remotePath(p)
	if err != nil {
		return nil, err
	}
	entries, err := c.propfindDepth(ctx, rel, "0")
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", p, err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("stat %s: not found", p)
	}
	e := entries[0]
	return &dest.Object{Path: c.relative(e.rel), Size: e.size, ModTime: e.modTime, IsDir: e.dir}, nil
}

func (c *client) Test(ctx context.Context) error {
	if err := c.mkdirAll(ctx, c.basePath); err != nil {
		return err
	}
	probe, err := c.remotePath(".backvault-write-test")
	if err != nil {
		return err
	}
	reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	probeBody := "backvault"
	req, err := c.request(reqCtx, http.MethodPut, c.urlFor(probe), strings.NewReader(probeBody), int64(len(probeBody)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/plain")
	resp, err := c.do(req, http.StatusCreated, http.StatusOK, http.StatusNoContent)
	if err != nil {
		return fmt.Errorf("base path is not writable: %w", err)
	}
	resp.Body.Close()
	return c.deletePath(ctx, probe)
}

func (c *client) Close() error {
	c.http.CloseIdleConnections()
	return nil
}

func (c *client) relative(rel string) string {
	if c.basePath == "" {
		return strings.Trim(rel, "/")
	}
	trimmed := strings.TrimPrefix(strings.Trim(rel, "/"), c.basePath)
	return strings.Trim(trimmed, "/")
}

type entry struct {
	rel     string
	size    int64
	modTime time.Time
	dir     bool
}

func (c *client) propfind(ctx context.Context, rel string) ([]entry, error) {
	return c.propfindDepth(ctx, rel, "1")
}

func (c *client) propfindDepth(ctx context.Context, rel, depth string) ([]entry, error) {
	const body = `<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:"><d:prop>` +
		`<d:resourcetype/><d:getcontentlength/><d:getlastmodified/></d:prop></d:propfind>`
	reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := c.request(reqCtx, "PROPFIND", c.urlFor(rel), strings.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Depth", depth)
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	resp, err := c.do(req, http.StatusMultiStatus, http.StatusOK)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var ms multistatus
	if err := xml.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&ms); err != nil {
		return nil, fmt.Errorf("parse propfind response: %w", err)
	}
	basePrefix := strings.TrimSuffix(c.base.EscapedPath(), "/")
	out := make([]entry, 0, len(ms.Responses))
	for _, r := range ms.Responses {
		href := strings.TrimSpace(r.Href)
		if href == "" {
			continue
		}
		hrefURL, err := url.Parse(href)
		if err != nil {
			continue
		}
		p := hrefURL.EscapedPath()
		if basePrefix != "" && strings.HasPrefix(p, basePrefix) {
			p = strings.TrimPrefix(p, basePrefix)
		}
		unescaped, err := url.PathUnescape(strings.Trim(p, "/"))
		if err != nil {
			unescaped = strings.Trim(p, "/")
		}
		e := entry{rel: unescaped}
		for _, ps := range r.Propstats {
			if ps.Status != "" && !strings.Contains(ps.Status, "200") {
				continue
			}
			if ps.Prop.ResourceType.Collection != nil {
				e.dir = true
			}
			if ps.Prop.ContentLength != "" {
				var n int64
				_, _ = fmt.Sscanf(ps.Prop.ContentLength, "%d", &n)
				e.size = n
			}
			if ps.Prop.LastModified != "" {
				if t, err := http.ParseTime(ps.Prop.LastModified); err == nil {
					e.modTime = t.UTC()
				}
			}
		}
		out = append(out, e)
	}
	return out, nil
}

type multistatus struct {
	XMLName   xml.Name      `xml:"DAV: multistatus"`
	Responses []davResponse `xml:"DAV: response"`
}

type davResponse struct {
	Href      string        `xml:"DAV: href"`
	Propstats []davPropstat `xml:"DAV: propstat"`
}

type davPropstat struct {
	Status string  `xml:"DAV: status"`
	Prop   davProp `xml:"DAV: prop"`
}

type davProp struct {
	ResourceType  davResourceType `xml:"DAV: resourcetype"`
	ContentLength string          `xml:"DAV: getcontentlength"`
	LastModified  string          `xml:"DAV: getlastmodified"`
}

type davResourceType struct {
	Collection *struct{} `xml:"DAV: collection"`
}

func redact(u *url.URL) string {
	if u == nil {
		return ""
	}
	clone := *u
	clone.User = nil
	return clone.String()
}

var (
	_ dest.Driver = (*Driver)(nil)
	_ dest.Client = (*client)(nil)
)
