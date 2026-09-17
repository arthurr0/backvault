package version

import (
	"runtime"
	"time"

	"github.com/arthurr0/backvault/internal/core"
)

var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

var startedAt = time.Now().UTC()

func Info() core.VersionInfo {
	return core.VersionInfo{
		Version:   Version,
		Commit:    Commit,
		BuildDate: BuildDate,
		GoVersion: runtime.Version(),
		StartedAt: startedAt,
	}
}
