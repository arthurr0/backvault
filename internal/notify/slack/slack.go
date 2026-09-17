package slack

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
		Kind:         "slack",
		Label:        "Slack",
		Description:  "Posts a colour-coded attachment to a Slack channel through an incoming webhook.",
		Icon:         "message-square",
		Category:     "Chat",
		Capabilities: []string{core.CapTest},
		Fields: []core.Field{
			{
				Name: "webhook_url", Label: "Incoming webhook URL", Type: core.FieldSecret, Secret: true,
				Required: true, Group: "Connection",
				Placeholder: "https://hooks.slack.com/services/T000/B000/XXXX",
				Help:        "Create it in Slack under Apps, Incoming Webhooks. The channel is decided when the webhook is created.",
			},
			{
				Name: "username", Label: "Bot name", Type: core.FieldString, Group: "Appearance",
				Advanced: true, Placeholder: "Backvault",
			},
			{
				Name: "icon_emoji", Label: "Icon emoji", Type: core.FieldString, Group: "Appearance",
				Advanced: true, Placeholder: ":lock:",
			},
			{
				Name: "channel", Label: "Channel override", Type: core.FieldString, Group: "Appearance",
				Advanced: true, Placeholder: "#alerts",
				Help: "Only works for legacy webhooks that allow overriding the channel.",
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
		return fmt.Errorf("incoming webhook url must be a full url, normally https://hooks.slack.com/services/...")
	}
	return nil
}

type payload struct {
	Text        string       `json:"text,omitempty"`
	Username    string       `json:"username,omitempty"`
	IconEmoji   string       `json:"icon_emoji,omitempty"`
	Channel     string       `json:"channel,omitempty"`
	Attachments []attachment `json:"attachments"`
}

type attachment struct {
	Fallback   string   `json:"fallback"`
	Color      string   `json:"color"`
	Title      string   `json:"title"`
	TitleLink  string   `json:"title_link,omitempty"`
	Text       string   `json:"text,omitempty"`
	Fields     []field  `json:"fields,omitempty"`
	Footer     string   `json:"footer"`
	Timestamp  int64    `json:"ts,omitempty"`
	MarkdownIn []string `json:"mrkdwn_in,omitempty"`
}

type field struct {
	Title string `json:"title"`
	Value string `json:"value"`
	Short bool   `json:"short"`
}

func (d *Driver) Send(ctx context.Context, cfg core.Config, ev notify.Event, log *slog.Logger) error {
	if err := d.Validate(cfg); err != nil {
		return err
	}
	m := message.Build(ev)
	att := attachment{
		Fallback:   m.Title,
		Color:      m.Color,
		Title:      m.Title,
		TitleLink:  m.Link,
		Footer:     "Backvault",
		Timestamp:  m.Time.Unix(),
		MarkdownIn: []string{"text", "fields"},
	}
	if m.Summary != "" && m.Summary != m.Title {
		att.Text = m.Summary
	}
	for _, f := range m.Fields {
		att.Fields = append(att.Fields, field{Title: f.Key, Value: f.Value, Short: len(f.Value) <= 32})
	}
	if m.Error != "" {
		att.Fields = append(att.Fields, field{Title: "Error", Value: "```" + truncate(m.Error, 1500) + "```", Short: false})
	}
	body := payload{
		Username:    cfg.String("username"),
		IconEmoji:   cfg.String("icon_emoji"),
		Channel:     cfg.String("channel"),
		Attachments: []attachment{att},
	}
	timeout := time.Duration(cfg.Int("timeout", 15)) * time.Second
	if err := httpx.PostJSON(ctx, httpx.Client(timeout), strings.TrimSpace(cfg.String("webhook_url")), body, nil); err != nil {
		return fmt.Errorf("slack webhook: %w", err)
	}
	log.Info("slack notification sent", "event", ev.Type)
	return nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

var _ notify.Driver = (*Driver)(nil)
