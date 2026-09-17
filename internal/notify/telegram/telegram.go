package telegram

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"strings"
	"time"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/notify"
	"github.com/arthurr0/backvault/internal/notify/internal/httpx"
	"github.com/arthurr0/backvault/internal/notify/internal/message"
)

const apiBase = "https://api.telegram.org"

type Driver struct{}

func New() *Driver { return &Driver{} }

func init() { notify.Register(New()) }

func (d *Driver) Spec() core.DriverSpec {
	return core.DriverSpec{
		Kind:         "telegram",
		Label:        "Telegram",
		Description:  "Sends a formatted message to a Telegram chat, group or channel through a bot.",
		Icon:         "send",
		Category:     "Chat",
		Capabilities: []string{core.CapTest},
		Fields: []core.Field{
			{
				Name: "bot_token", Label: "Bot token", Type: core.FieldSecret, Secret: true,
				Required: true, Group: "Connection",
				Placeholder: "123456789:AAG...",
				Help:        "Create a bot with @BotFather and paste the token here.",
			},
			{
				Name: "chat_id", Label: "Chat id", Type: core.FieldString, Required: true, Group: "Connection",
				Placeholder: "-1001234567890",
				Help:        "Numeric id of the chat, group or channel. Send a message to the bot and read it from getUpdates, or use @userinfobot.",
			},
			{
				Name: "message_thread_id", Label: "Topic id", Type: core.FieldString, Group: "Connection",
				Advanced: true, Placeholder: "12",
				Help: "Only for forum groups with topics.",
			},
			{
				Name: "disable_notification", Label: "Send silently", Type: core.FieldBool, Default: false,
				Group: "Options",
				Help:  "Delivers the message without a sound.",
			},
			{
				Name: "api_base", Label: "API base URL", Type: core.FieldString, Default: apiBase,
				Group: "Advanced", Advanced: true,
				Help: "Change only when you run a local Bot API server.",
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
	if !strings.Contains(cfg.String("bot_token"), ":") {
		return fmt.Errorf("bot token should look like 123456789:AA...")
	}
	return nil
}

type payload struct {
	ChatID              string `json:"chat_id"`
	Text                string `json:"text"`
	ParseMode           string `json:"parse_mode"`
	DisableNotification bool   `json:"disable_notification,omitempty"`
	MessageThreadID     string `json:"message_thread_id,omitempty"`
	LinkPreviewOptions  struct {
		IsDisabled bool `json:"is_disabled"`
	} `json:"link_preview_options"`
}

func (d *Driver) Send(ctx context.Context, cfg core.Config, ev notify.Event, log *slog.Logger) error {
	if err := d.Validate(cfg); err != nil {
		return err
	}
	m := message.Build(ev)
	body := payload{
		ChatID:              cfg.String("chat_id"),
		Text:                render(m),
		ParseMode:           "HTML",
		DisableNotification: cfg.Bool("disable_notification", false),
		MessageThreadID:     cfg.String("message_thread_id"),
	}
	body.LinkPreviewOptions.IsDisabled = true
	base := strings.TrimRight(cfg.StringOr("api_base", apiBase), "/")
	target := base + "/bot" + cfg.String("bot_token") + "/sendMessage"
	timeout := time.Duration(cfg.Int("timeout", 15)) * time.Second
	if err := httpx.PostJSON(ctx, httpx.Client(timeout), target, body, nil); err != nil {
		return fmt.Errorf("telegram sendMessage: %w", scrub(cfg, err))
	}
	log.Info("telegram notification sent", "event", ev.Type, "chat", cfg.String("chat_id"))
	return nil
}

func render(m message.Model) string {
	var b strings.Builder
	b.WriteString("<b>" + esc(m.Title) + "</b>\n")
	if m.Summary != "" && m.Summary != m.Title {
		b.WriteString(esc(m.Summary) + "\n")
	}
	b.WriteString("\n")
	for _, f := range m.Fields {
		b.WriteString(esc(f.Key) + ": <b>" + esc(f.Value) + "</b>\n")
	}
	if m.Error != "" {
		b.WriteString("\n<pre>" + esc(truncate(m.Error, 1500)) + "</pre>\n")
	}
	if m.Link != "" {
		b.WriteString("\n<a href=\"" + esc(m.Link) + "\">Open in Backvault</a>")
	}
	return truncate(b.String(), 4000)
}

func esc(s string) string {
	return html.EscapeString(s)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func scrub(cfg core.Config, err error) error {
	if err == nil {
		return nil
	}
	token := cfg.String("bot_token")
	if len(token) < 3 {
		return err
	}
	msg := strings.ReplaceAll(err.Error(), token, "***")
	if i := strings.Index(token, ":"); i > 0 {
		msg = strings.ReplaceAll(msg, token[i+1:], "***")
	}
	return fmt.Errorf("%s", msg)
}

var _ notify.Driver = (*Driver)(nil)
