package testevent

import (
	"io"
	"log/slog"
	"time"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/notify"
)

func Logger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func Failed() notify.Event {
	started := time.Date(2024, 5, 4, 3, 0, 0, 0, time.UTC)
	finished := started.Add(92 * time.Second)
	return notify.Event{
		Type:     core.EventRunFailed,
		Severity: notify.SeverityError,
		Time:     finished,
		SiteName: "Acme Backups",
		BaseURL:  "https://backvault.example.com",
		Job: &core.Job{
			ID: "job1", Slug: "db-nightly", Name: "Database nightly",
			DestinationNames: []string{"Hetzner Box", "MinIO"},
		},
		Run: &core.Run{
			ID: "run1", JobSlug: "db-nightly", JobName: "Database nightly",
			Kind: core.RunBackup, Status: core.RunFailed,
			StartedAt: &started, FinishedAt: &finished,
			DurationMS: 92000, Bytes: 5 * 1024 * 1024, RawBytes: 18 * 1024 * 1024,
			Error: "pg_dump exited with code 1: could not connect to server",
		},
		Fields: map[string]string{"attempt": "2"},
	}
}

func Success() notify.Event {
	started := time.Date(2024, 5, 4, 3, 0, 0, 0, time.UTC)
	finished := started.Add(30 * time.Second)
	return notify.Event{
		Type:     core.EventRunSuccess,
		Severity: notify.SeverityInfo,
		Time:     finished,
		SiteName: "Acme Backups",
		BaseURL:  "https://backvault.example.com",
		Job: &core.Job{
			ID: "job1", Slug: "files-daily", Name: "Website files",
			DestinationNames: []string{"Local disk"},
		},
		Run: &core.Run{
			ID: "run2", JobSlug: "files-daily", JobName: "Website files",
			Kind: core.RunBackup, Status: core.RunSuccess,
			StartedAt: &started, FinishedAt: &finished,
			DurationMS: 30000, Bytes: 1024 * 1024,
		},
	}
}
