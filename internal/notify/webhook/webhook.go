package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/notify"
	"github.com/arthurr0/backvault/internal/notify/internal/httpx"
)

const SignatureHeader = "X-Backvault-Signature"

type Driver struct{}

func New() *Driver { return &Driver{} }

func init() { notify.Register(New()) }

func (d *Driver) Spec() core.DriverSpec {
	return core.DriverSpec{
		Kind:         "webhook",
		Label:        "Webhook",
		Description:  "Posts the raw event as JSON to any HTTP endpoint, optionally signed with HMAC-SHA256.",
		Icon:         "webhook",
		Category:     "Custom",
		Capabilities: []string{core.CapTest},
		Fields: []core.Field{
			{
				Name: "url", Label: "URL", Type: core.FieldString, Required: true, Group: "Request",
				Placeholder: "https://hooks.example.com/backvault",
			},
			{
				Name: "method", Label: "Method", Type: core.FieldSelect, Default: "POST", Group: "Request",
				Options: []core.FieldOption{
					{Value: "POST", Label: "POST"},
					{Value: "PUT", Label: "PUT"},
					{Value: "PATCH", Label: "PATCH"},
				},
			},
			{
				Name: "headers", Label: "Extra headers", Type: core.FieldStringList, Group: "Request",
				Placeholder: "Authorization: Bearer abc123",
				Help:        "One Key: Value per line.",
			},
			{
				Name: "secret", Label: "Signing secret", Type: core.FieldSecret, Secret: true, Group: "Request",
				Help: "When set, the request carries " + SignatureHeader + " with the lowercase hex HMAC-SHA256 of the body.",
			},
			{
				Name: "timeout", Label: "Timeout (seconds)", Type: core.FieldInt, Default: 15,
				Group: "Advanced", Advanced: true,
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
		return fmt.Errorf("url must be a full url such as https://hooks.example.com/backvault")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("url must use http or https")
	}
	switch strings.ToUpper(cfg.StringOr("method", "POST")) {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
	default:
		return fmt.Errorf("method must be POST, PUT or PATCH")
	}
	for _, h := range cfg.StringList("headers") {
		if !strings.Contains(h, ":") {
			return fmt.Errorf("header %q must look like Key: Value", h)
		}
	}
	return nil
}

func (d *Driver) Send(ctx context.Context, cfg core.Config, ev notify.Event, log *slog.Logger) error {
	if err := d.Validate(cfg); err != nil {
		return err
	}
	body, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("encode event: %w", err)
	}
	method := strings.ToUpper(cfg.StringOr("method", "POST"))
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimSpace(cfg.String("url")), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Backvault")
	req.Header.Set("X-Backvault-Event", ev.Type)
	req.Header.Set("X-Backvault-Timestamp", strconv.FormatInt(timestamp(ev).Unix(), 10))
	for _, h := range cfg.StringList("headers") {
		parts := strings.SplitN(h, ":", 2)
		if len(parts) != 2 {
			continue
		}
		req.Header.Set(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
	}
	if secret := cfg.String("secret"); secret != "" {
		req.Header.Set(SignatureHeader, Sign(secret, body))
	}
	timeout := time.Duration(cfg.Int("timeout", 15)) * time.Second
	if err := httpx.Do(httpx.Client(timeout), req); err != nil {
		return err
	}
	log.Info("webhook delivered", "event", ev.Type, "url", httpx.Safe(req.URL))
	return nil
}

func Sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func timestamp(ev notify.Event) time.Time {
	if ev.Time.IsZero() {
		return time.Now().UTC()
	}
	return ev.Time
}

var _ notify.Driver = (*Driver)(nil)
