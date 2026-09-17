package drivers

import (
	"context"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/dest"
	"github.com/arthurr0/backvault/internal/notify"
	"github.com/arthurr0/backvault/internal/source"

	_ "github.com/arthurr0/backvault/internal/dest/local"
	_ "github.com/arthurr0/backvault/internal/dest/s3"
	_ "github.com/arthurr0/backvault/internal/dest/sftp"
	_ "github.com/arthurr0/backvault/internal/dest/webdav"

	_ "github.com/arthurr0/backvault/internal/notify/discord"
	_ "github.com/arthurr0/backvault/internal/notify/email"
	_ "github.com/arthurr0/backvault/internal/notify/ntfy"
	_ "github.com/arthurr0/backvault/internal/notify/slack"
	_ "github.com/arthurr0/backvault/internal/notify/telegram"
	_ "github.com/arthurr0/backvault/internal/notify/webhook"

	_ "github.com/arthurr0/backvault/internal/source/command"
	_ "github.com/arthurr0/backvault/internal/source/docker"
	_ "github.com/arthurr0/backvault/internal/source/files"
	_ "github.com/arthurr0/backvault/internal/source/mongodb"
	_ "github.com/arthurr0/backvault/internal/source/mysql"
	_ "github.com/arthurr0/backvault/internal/source/postgres"
	_ "github.com/arthurr0/backvault/internal/source/push"
	_ "github.com/arthurr0/backvault/internal/source/redis"
	_ "github.com/arthurr0/backvault/internal/source/sqlite"
	_ "github.com/arthurr0/backvault/internal/source/ssh"
)

func Register() {}

func Sources() []core.DriverSpec { return source.Specs() }

func Destinations() []core.DriverSpec { return dest.Specs() }

func Notifiers() []core.DriverSpec { return notify.Specs() }

func Tools() []core.ToolStatus {
	usage := map[string]map[string]bool{}
	add := func(spec core.DriverSpec) {
		for _, tool := range spec.Tools {
			if usage[tool] == nil {
				usage[tool] = map[string]bool{}
			}
			usage[tool][spec.Label] = true
		}
	}
	for _, s := range source.Specs() {
		add(s)
	}
	for _, s := range dest.Specs() {
		add(s)
	}
	for _, s := range notify.Specs() {
		add(s)
	}
	names := make([]string, 0, len(usage))
	for name := range usage {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]core.ToolStatus, 0, len(names))
	for _, name := range names {
		users := make([]string, 0, len(usage[name]))
		for u := range usage[name] {
			users = append(users, u)
		}
		sort.Strings(users)
		status := core.ToolStatus{Name: name, UsedBy: users}
		if path, err := exec.LookPath(name); err == nil {
			status.Available = true
			status.Path = path
			status.Version = toolVersion(path)
		}
		out = append(out, status)
	}
	return out
}

var versionPattern = regexp.MustCompile(`\d+\.\d+(\.\d+)?`)

func toolVersion(path string) string {
	for _, flag := range []string{"--version", "-V", "version"} {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		out, err := exec.CommandContext(ctx, path, flag).CombinedOutput()
		cancel()
		if err != nil {
			continue
		}
		line := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
		if line == "" {
			continue
		}
		if m := versionPattern.FindString(line); m != "" {
			return m
		}
		return line
	}
	return ""
}
