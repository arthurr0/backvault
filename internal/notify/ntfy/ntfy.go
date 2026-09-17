package ntfy

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

const defaultServer = "https://ntfy.sh"

type Driver struct{}

func New() *Driver { return &Driver{} }

func init() { notify.Register(New()) }

func (d *Driver) Spec() core.DriverSpec {
	return core.DriverSpec{
		Kind:         "ntfy",
		Label:        "ntfy",
		Description:  "Push notification to a ntfy topic, on ntfy.sh or on your own server.",
		Icon:         "bell",
		Category:     "Push",
		Capabilities: []string{core.CapTest},
		Fields: []core.Field{
			{
				Name: "server_url", Label: "Server URL", Type: core.FieldString, Default: defaultServer,
				Group: "Connection", Placeholder: defaultServer,
			},
			{
				Name: "topic", Label: "Topic", Type: core.FieldString, Required: true, Group: "Connection",
				Placeholder: "backvault-backups",
				Help:        "Anyone who knows a public topic name can read it. Pick something long and random, or use an access token on your own server.",
			},
			{
				Name: "token", Label: "Access token", Type: core.FieldSecret, Secret: true, Group: "Connection",
				Placeholder: "tk_...",
				Help:        "Optional bearer token for protected topics.",
			},
			{
				Name: "tags", Label: "Extra tags", Type: core.FieldStringList, Group: "Options",
				Placeholder: "floppy_disk",
				Help:        "Emoji short codes or plain words added to every message, one per line.",
			},
			{
				Name: "priority_success", Label: "Priority for success", Type: core.FieldSelect, Default: "low",
				Group: "Options", Options: priorityOptions(),
			},
			{
				Name: "priority_warning", Label: "Priority for warnings", Type: core.FieldSelect, Default: "default",
				Group: "Options", Options: priorityOptions(),
			},
			{
				Name: "priority_error", Label: "Priority for failures", Type: core.FieldSelect, Default: "high",
				Group: "Options", Options: priorityOptions(),
			},
			{
				Name: "timeout", Label: "Timeout (seconds)", Type: core.FieldInt, Default: 15,
				Group: "Advanced", Advanced: true,
			},
		},
	}
}

func priorityOptions() []core.FieldOption {
	return []core.FieldOption{
		{Value: "min", Label: "Min"},
		{Value: "low", Label: "Low"},
		{Value: "default", Label: "Default"},
		{Value: "high", Label: "High"},
		{Value: "urgent", Label: "Urgent"},
	}
}

func (d *Driver) Validate(cfg core.Config) error {
	if err := core.ValidateRequired(d.Spec(), cfg); err != nil {
		return err
	}
	u, err := url.Parse(strings.TrimSpace(cfg.StringOr("server_url", defaultServer)))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("server url must be a full url such as https://ntfy.sh")
	}
	if strings.ContainsAny(cfg.String("topic"), "/ ") {
		return fmt.Errorf("topic must not contain spaces or slashes")
	}
	return nil
}

type payload struct {
	Topic    string   `json:"topic"`
	Title    string   `json:"title"`
	Message  string   `json:"message"`
	Priority int      `json:"priority,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Click    string   `json:"click,omitempty"`
	Markdown bool     `json:"markdown,omitempty"`
}

func (d *Driver) Send(ctx context.Context, cfg core.Config, ev notify.Event, log *slog.Logger) error {
	if err := d.Validate(cfg); err != nil {
		return err
	}
	m := message.Build(ev)
	body := payload{
		Topic:    cfg.String("topic"),
		Title:    m.Title,
		Message:  m.Text(),
		Priority: priority(cfg, m.Severity),
		Tags:     tags(cfg, m.Severity),
		Click:    m.Link,
	}
	headers := map[string]string{}
	if token := cfg.String("token"); token != "" {
		headers["Authorization"] = "Bearer " + token
	}
	server := strings.TrimRight(cfg.StringOr("server_url", defaultServer), "/")
	timeout := time.Duration(cfg.Int("timeout", 15)) * time.Second
	if err := httpx.PostJSON(ctx, httpx.Client(timeout), server, body, headers); err != nil {
		return fmt.Errorf("ntfy publish: %w", err)
	}
	log.Info("ntfy notification sent", "event", ev.Type, "topic", cfg.String("topic"))
	return nil
}

func priority(cfg core.Config, severity notify.Severity) int {
	var name string
	switch severity {
	case notify.SeverityError:
		name = cfg.StringOr("priority_error", "high")
	case notify.SeverityWarning:
		name = cfg.StringOr("priority_warning", "default")
	default:
		name = cfg.StringOr("priority_success", "low")
	}
	switch name {
	case "min":
		return 1
	case "low":
		return 2
	case "default":
		return 3
	case "high":
		return 4
	case "urgent", "max":
		return 5
	}
	return 3
}

func tags(cfg core.Config, severity notify.Severity) []string {
	var out []string
	switch severity {
	case notify.SeverityError:
		out = append(out, "rotating_light")
	case notify.SeverityWarning:
		out = append(out, "warning")
	default:
		out = append(out, "white_check_mark")
	}
	return append(out, cfg.StringList("tags")...)
}

var _ notify.Driver = (*Driver)(nil)
