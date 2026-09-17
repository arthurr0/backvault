package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/arthurr0/backvault/internal/core"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "backvault.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestMigrationsIdempotent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "backvault.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	if err := s2.Ping(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestUsersAndSessions(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	n, err := s.Users.Count(ctx)
	if err != nil || n != 0 {
		t.Fatalf("count = %d, err = %v", n, err)
	}
	u, err := s.Users.Create(ctx, core.User{Email: "Admin@Example.COM", Name: "Admin", Role: core.RoleAdmin}, "hash")
	if err != nil {
		t.Fatal(err)
	}
	if u.Email != "admin@example.com" {
		t.Fatalf("email = %q", u.Email)
	}
	if _, err := s.Users.Create(ctx, core.User{Email: "admin@example.com", Role: core.RoleAdmin}, "x"); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	rec, err := s.Users.GetByEmail(ctx, "ADMIN@example.com")
	if err != nil || rec.PasswordHash != "hash" {
		t.Fatalf("get by email: %v %q", err, rec.PasswordHash)
	}
	admins, err := s.Users.CountAdmins(ctx)
	if err != nil || admins != 1 {
		t.Fatalf("admins = %d %v", admins, err)
	}

	now := time.Now().UTC()
	sess := Session{ID: "abc", UserID: u.ID, CreatedAt: now, ExpiresAt: now.Add(time.Hour), LastSeenAt: now}
	if err := s.Sessions.Create(ctx, sess); err != nil {
		t.Fatal(err)
	}
	got, err := s.Sessions.Get(ctx, "abc")
	if err != nil || got.UserID != u.ID {
		t.Fatalf("session: %v %+v", err, got)
	}
	if _, err := s.Sessions.DeleteExpired(ctx, now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Sessions.Get(ctx, "abc"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}

	if err := s.Users.Delete(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Users.Delete(ctx, u.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func seedJob(t *testing.T, s *Store) (core.Source, core.Destination, core.Job) {
	t.Helper()
	ctx := context.Background()
	src, err := s.Sources.Create(ctx, core.Source{Name: "files", Kind: "files", Config: core.Config{"paths": []string{"/etc"}}})
	if err != nil {
		t.Fatal(err)
	}
	dst, err := s.Destinations.Create(ctx, core.Destination{Name: "disk", Kind: "local", Config: core.Config{"path": "/backups"}})
	if err != nil {
		t.Fatal(err)
	}
	job, err := s.Jobs.Create(ctx, core.Job{
		Slug: "nightly", Name: "Nightly", SourceID: src.ID, DestinationIDs: []string{dst.ID},
		Schedule: "0 2 * * *", Timezone: "UTC", Enabled: true,
		Compression: core.CompressionZstd, Encryption: core.EncryptionNone,
		Retention: core.Retention{KeepLast: 3},
	})
	if err != nil {
		t.Fatal(err)
	}
	return src, dst, job
}

func TestJobsComputedFields(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	src, dst, job := seedJob(t, s)

	run, err := s.Runs.Create(ctx, core.Run{JobID: job.ID, JobSlug: job.Slug, JobName: job.Name, Kind: core.RunBackup, Trigger: core.TriggerManual, Status: core.RunSuccess})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Artifacts.Create(ctx, core.Artifact{
		JobID: job.ID, JobSlug: job.Slug, JobName: job.Name, RunID: run.ID,
		DestinationID: dst.ID, DestinationName: dst.Name, DestinationKind: dst.Kind,
		Path: "nightly/a.tar.zst", Filename: "a.tar.zst", Size: 1024, Status: core.ArtifactPresent,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.JobStates.SetLastRun(ctx, job.ID, run.Summary(), &run.QueuedAt); err != nil {
		t.Fatal(err)
	}
	next := time.Now().UTC().Add(time.Hour)
	if err := s.JobStates.SetNextRun(ctx, job.ID, &next); err != nil {
		t.Fatal(err)
	}

	got, err := s.Jobs.Get(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SourceName != src.Name || got.SourceKind != src.Kind {
		t.Fatalf("source labels: %+v", got)
	}
	if len(got.DestinationNames) != 1 || got.DestinationNames[0] != dst.Name {
		t.Fatalf("destination names: %v", got.DestinationNames)
	}
	if got.LastRun == nil || got.LastRun.ID != run.ID {
		t.Fatalf("last run: %+v", got.LastRun)
	}
	if got.NextRunAt == nil {
		t.Fatal("next run missing")
	}
	if got.ArtifactCount != 1 || got.TotalBytes != 1024 {
		t.Fatalf("totals: %d %d", got.ArtifactCount, got.TotalBytes)
	}

	bySlug, err := s.Jobs.GetBySlug(ctx, "nightly")
	if err != nil || bySlug.ID != job.ID {
		t.Fatalf("by slug: %v", err)
	}
	byRef, err := s.Jobs.GetByIDOrSlug(ctx, "nightly")
	if err != nil || byRef.ID != job.ID {
		t.Fatalf("by ref: %v", err)
	}

	d, err := s.Destinations.Get(ctx, dst.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.JobCount != 1 || d.ArtifactCount != 1 || d.UsedBytes != 1024 {
		t.Fatalf("destination computed: %+v", d)
	}
	srcGot, err := s.Sources.Get(ctx, src.ID)
	if err != nil {
		t.Fatal(err)
	}
	if srcGot.JobCount != 1 {
		t.Fatalf("source job count = %d", srcGot.JobCount)
	}
	if err := s.Sources.Delete(ctx, src.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected conflict deleting used source, got %v", err)
	}
}

func TestRunListFiltersAndPagination(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	_, _, job := seedJob(t, s)

	base := time.Now().UTC().Add(-10 * time.Hour)
	for i := 0; i < 7; i++ {
		status := core.RunSuccess
		if i%3 == 0 {
			status = core.RunFailed
		}
		if _, err := s.Runs.Create(ctx, core.Run{
			JobID: job.ID, JobSlug: job.Slug, Kind: core.RunBackup, Trigger: core.TriggerSchedule,
			Status: status, QueuedAt: base.Add(time.Duration(i) * time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
	}
	items, total, err := s.Runs.List(ctx, RunFilter{JobID: job.ID, Page: Page{Limit: 3}})
	if err != nil {
		t.Fatal(err)
	}
	if total != 7 || len(items) != 3 {
		t.Fatalf("total = %d, items = %d", total, len(items))
	}
	if !items[0].QueuedAt.After(items[1].QueuedAt) {
		t.Fatal("expected newest first")
	}
	failed, total, err := s.Runs.List(ctx, RunFilter{Status: string(core.RunFailed)})
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(failed) != 3 {
		t.Fatalf("failed total = %d", total)
	}
	since, _, err := s.Runs.List(ctx, RunFilter{Since: base.Add(4 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(since) != 3 {
		t.Fatalf("since = %d", len(since))
	}
}

func TestRunLogs(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	_, _, job := seedJob(t, s)
	run, err := s.Runs.Create(ctx, core.Run{JobID: job.ID, Kind: core.RunBackup, Trigger: core.TriggerManual, Status: core.RunRunning})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Runs.AppendLog(ctx, run.ID, "line one\n"); err != nil {
		t.Fatal(err)
	}
	seq, err := s.Runs.AppendLog(ctx, run.ID, "line two\n")
	if err != nil {
		t.Fatal(err)
	}
	if seq != 2 {
		t.Fatalf("seq = %d", seq)
	}
	full, err := s.Runs.Log(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if full != "line one\nline two\n" {
		t.Fatalf("log = %q", full)
	}
	tail, err := s.Runs.LogChunks(ctx, run.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(tail) != 1 || tail[0].Text != "line two\n" {
		t.Fatalf("tail = %+v", tail)
	}
}

func TestSettingsAndAudit(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	def, err := s.Settings.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if def.MaxConcurrentRuns != 2 {
		t.Fatalf("default concurrency = %d", def.MaxConcurrentRuns)
	}
	def.SiteName = "Backups"
	def.MaxConcurrentRuns = 0
	saved, err := s.Settings.Save(ctx, def)
	if err != nil {
		t.Fatal(err)
	}
	if saved.MaxConcurrentRuns != 2 {
		t.Fatalf("normalized concurrency = %d", saved.MaxConcurrentRuns)
	}
	again, err := s.Settings.Get(ctx)
	if err != nil || again.SiteName != "Backups" {
		t.Fatalf("settings reload: %v %+v", err, again)
	}

	if err := s.Audit.Record(ctx, core.AuditEntry{Action: "job.create", ObjectType: "job", ObjectName: "Nightly", ActorLabel: "admin"}); err != nil {
		t.Fatal(err)
	}
	entries, total, err := s.Audit.List(ctx, AuditFilter{Action: "job.create"})
	if err != nil || total != 1 || len(entries) != 1 {
		t.Fatalf("audit: %v %d", err, total)
	}
}

func TestCleanup(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	_, _, job := seedJob(t, s)
	old := time.Now().UTC().AddDate(0, 0, -200)
	if _, err := s.Runs.Create(ctx, core.Run{JobID: job.ID, Kind: core.RunBackup, Trigger: core.TriggerSchedule, Status: core.RunSuccess, QueuedAt: old}); err != nil {
		t.Fatal(err)
	}
	if err := s.Audit.Record(ctx, core.AuditEntry{Action: "old", Time: old}); err != nil {
		t.Fatal(err)
	}
	settings, err := s.Settings.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	settings.AuditHistoryDays = 30
	res, err := s.Cleanup(ctx, settings, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if res.Runs != 1 || res.Audit != 1 {
		t.Fatalf("cleanup = %+v", res)
	}
}

func TestDashboard(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	_, dst, job := seedJob(t, s)
	run, err := s.Runs.Create(ctx, core.Run{JobID: job.ID, JobSlug: job.Slug, Kind: core.RunBackup, Trigger: core.TriggerSchedule, Status: core.RunSuccess, Bytes: 2048})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Artifacts.Create(ctx, core.Artifact{JobID: job.ID, RunID: run.ID, DestinationID: dst.ID, DestinationName: dst.Name, Path: "p", Filename: "f", Size: 2048}); err != nil {
		t.Fatal(err)
	}
	stats, err := s.Stats.Dashboard(ctx, time.Now().UTC(), 30, 10)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Jobs != 1 || stats.JobsEnabled != 1 {
		t.Fatalf("jobs = %+v", stats)
	}
	if stats.Runs24hSuccess != 1 {
		t.Fatalf("24h success = %d", stats.Runs24hSuccess)
	}
	if stats.Artifacts != 1 || stats.TotalBytes != 2048 {
		t.Fatalf("artifacts = %d bytes = %d", stats.Artifacts, stats.TotalBytes)
	}
	if len(stats.Daily) != 30 {
		t.Fatalf("daily = %d", len(stats.Daily))
	}
	if len(stats.Destinations) != 1 || stats.Destinations[0].Bytes != 2048 {
		t.Fatalf("destinations = %+v", stats.Destinations)
	}
	if len(stats.RecentRuns) != 1 {
		t.Fatalf("recent runs = %d", len(stats.RecentRuns))
	}
}

func TestJobDeleteKeepsArtifacts(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	_, dst, job := seedJob(t, s)
	if _, err := s.Artifacts.Create(ctx, core.Artifact{JobID: job.ID, JobSlug: job.Slug, JobName: job.Name, DestinationID: dst.ID, Path: "p", Filename: "f", Size: 10}); err != nil {
		t.Fatal(err)
	}
	if err := s.Jobs.Delete(ctx, job.ID, false); err != nil {
		t.Fatal(err)
	}
	items, total, err := s.Artifacts.List(ctx, ArtifactFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || items[0].JobName != job.Name {
		t.Fatalf("artifacts after job delete: %d %+v", total, items)
	}
}

func TestTokens(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	tok, err := s.Tokens.Create(ctx, core.APIToken{Name: "ci", Prefix: "bvt_abcd", Scopes: []string{core.ScopeIngest}, JobSlugs: []string{"nightly"}}, "hash1")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Tokens.GetByHash(ctx, "hash1")
	if err != nil || got.ID != tok.ID {
		t.Fatalf("by hash: %v", err)
	}
	if len(got.Scopes) != 1 || got.Scopes[0] != core.ScopeIngest {
		t.Fatalf("scopes = %v", got.Scopes)
	}
	list, err := s.Tokens.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %d", err, len(list))
	}
	if err := s.Tokens.Delete(ctx, tok.ID); err != nil {
		t.Fatal(err)
	}
}

func TestChannels(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	c, err := s.Channels.Create(ctx, core.NotificationChannel{Name: "ops", Kind: "slack", Enabled: true, Config: core.Config{"url": "https://hooks"}, Events: []string{core.EventRunFailed}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Channels.Create(ctx, core.NotificationChannel{Name: "ops", Kind: "slack"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	c.Enabled = false
	if _, err := s.Channels.Update(ctx, c); err != nil {
		t.Fatal(err)
	}
	got, err := s.Channels.Get(ctx, c.ID)
	if err != nil || got.Enabled {
		t.Fatalf("channel: %v %+v", err, got)
	}
}
