package engine

import (
	"context"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/dest"
	"github.com/arthurr0/backvault/internal/notify"
	"github.com/arthurr0/backvault/internal/source"
)

var knownTools = map[string][]string{
	"pg_dump":      {"postgres"},
	"pg_dumpall":   {"postgres"},
	"pg_restore":   {"postgres"},
	"psql":         {"postgres"},
	"mysqldump":    {"mysql"},
	"mariadb-dump": {"mysql"},
	"mysql":        {"mysql"},
	"mongodump":    {"mongodb"},
	"mongorestore": {"mongodb"},
	"redis-cli":    {"redis"},
	"sqlite3":      {"sqlite"},
	"docker":       {"docker"},
	"ssh":          {"ssh"},
	"tar":          {"files", "docker"},
	"zstd":         {"pack"},
	"gzip":         {"pack"},
	"age":          {"pack"},
}

var versionArgs = map[string][]string{
	"redis-cli": {"--version"},
	"docker":    {"--version"},
	"ssh":       {"-V"},
	"age":       {"--version"},
}

func Tools(ctx context.Context) []core.ToolStatus {
	usedBy := map[string]map[string]struct{}{}
	add := func(tool, kind string) {
		tool = strings.TrimSpace(tool)
		if tool == "" {
			return
		}
		if usedBy[tool] == nil {
			usedBy[tool] = map[string]struct{}{}
		}
		if kind != "" {
			usedBy[tool][kind] = struct{}{}
		}
	}
	for tool, kinds := range knownTools {
		for _, k := range kinds {
			add(tool, k)
		}
	}
	for _, spec := range source.Specs() {
		for _, tool := range spec.Tools {
			add(tool, spec.Kind)
		}
	}
	for _, spec := range dest.Specs() {
		for _, tool := range spec.Tools {
			add(tool, spec.Kind)
		}
	}
	for _, spec := range notify.Specs() {
		for _, tool := range spec.Tools {
			add(tool, spec.Kind)
		}
	}

	names := make([]string, 0, len(usedBy))
	for name := range usedBy {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]core.ToolStatus, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		kinds := make([]string, 0, len(usedBy[name]))
		for k := range usedBy[name] {
			kinds = append(kinds, k)
		}
		sort.Strings(kinds)
		out[i] = core.ToolStatus{Name: name, UsedBy: kinds}

		wg.Add(1)
		go func(idx int, tool string) {
			defer wg.Done()
			path, err := exec.LookPath(tool)
			if err != nil {
				return
			}
			out[idx].Available = true
			out[idx].Path = path
			out[idx].Version = toolVersion(ctx, tool, path)
		}(i, name)
	}
	wg.Wait()
	return out
}

func toolVersion(ctx context.Context, tool, path string) string {
	args, ok := versionArgs[tool]
	if !ok {
		args = []string{"--version"}
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	output, err := cmd.CombinedOutput()
	if err != nil && len(output) == 0 {
		return ""
	}
	line := strings.TrimSpace(strings.SplitN(strings.TrimSpace(string(output)), "\n", 2)[0])
	if len(line) > 120 {
		line = line[:120]
	}
	return line
}
