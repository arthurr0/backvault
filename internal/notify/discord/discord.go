package discord

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/notify"
	"github.com/arthurr0/backvault/internal/notify/internal/httpx"
	"github.com/arthurr0/backvault/internal/notify/internal/message"
)

type Driver struct{}

func New() *Driver { return &Driver{} }

func init() { notify.Register(New()) }

func (d *Driver) Spec() core.DriverSpec {
	return core.DriverSpec{
		Kind:         "discord",
		Label:        "Discord",
		Description:  "Posts a colour-coded embed to a Discord channel through a channel webhook.",
		Icon:         "message-circle",
		Category:     "Chat",
		Capabilities: []string{core.CapTest},
		Fields: []core.Field{
			{
				Name: "webhook_url", Label: "Webhook URL", Type: core.FieldSecret, Secret: true,
				Required: true, Group: "Connection",
				Placeholder: "https://discord.com/api/webhooks/000/xxxx",
				Help:        "Channel settings, Integrations, Webhooks, Copy Webhook URL.",
			},
			{
				Name: "username", Label: "Bot name", Type: core.FieldString, Group: "Appearance",
				Advanced: true, Placeholder: "Backvault",
			},
			{
				Name: "avatar_url", Label: "Avatar URL", Type: core.FieldString, Group: "Appearance",
				Advanced: true,
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
	u, err := url.Parse(strings.TrimSpace(cfg.String("webhook_url")))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("webhook url must be a full url, normally https://discord.com/api/webhooks/...")
	}
	return nil
}

type payload struct {
	Username  string  `json:"username,omitempty"`
	AvatarURL string  `json:"avatar_url,omitempty"`
	Embeds    []embed `json:"embeds"`
}

type embed struct {
	Title       string       `json:"title"`
	URL         string       `json:"url,omitempty"`
	Description string       `json:"description,omitempty"`
	Color       int          `json:"color"`
	Fields      []embedField `json:"fields,omitempty"`
	Footer      embedFooter  `json:"footer"`
	Timestamp   string       `json:"timestamp"`
}

type embedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

type embedFooter struct {
	Text string `json:"text"`
}

func (d *Driver) Send(ctx context.Context, cfg core.Config, ev notify.Event, log *slog.Logger) error {
	if err := d.Validate(cfg); err != nil {
		return err
	}
	m := message.Build(ev)
	e := embed{
		Title:     truncate(m.Title, 256),
		URL:       m.Link,
		Color:     m.ColorInt(),
		Footer:    embedFooter{Text: "Backvault"},
		Timestamp: m.Time.UTC().Format(time.RFC3339),
	}
	if m.Summary != "" && m.Summary != m.Title {
		e.Description = truncate(m.Summary, 2000)
	}
	for _, f := range m.Fields {
		e.Fields = append(e.Fields, embedField{Name: f.Key, Value: truncate(f.Value, 1024), Inline: len(f.Value) <= 32})
	}
	if m.Error != "" {
		e.Fields = append(e.Fields, embedField{Name: "Error", Value: "```" + truncate(m.Error, 1000) + "```"})
	}
	body := payload{
		Username:  cfg.String("username"),
		AvatarURL: cfg.String("avatar_url"),
		Embeds:    []embed{e},
	}
	timeout := time.Duration(cfg.Int("timeout", 15)) * time.Second
	if err := httpx.PostJSON(ctx, httpx.Client(timeout), strings.TrimSpace(cfg.String("webhook_url")), body, nil); err != nil {
		return fmt.Errorf("discord webhook: %w", err)
	}
	log.Info("discord notification sent", "event", ev.Type)
	return nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

var _ notify.Driver = (*Driver)(nil)
