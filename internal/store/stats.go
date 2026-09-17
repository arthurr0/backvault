package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/arthurr0/backvault/internal/core"
)

type StatsRepo struct{ s *Store }

func (r *StatsRepo) Dashboard(ctx context.Context, now time.Time, dailyDays, recentRuns int) (core.DashboardStats, error) {
	var out core.DashboardStats
	now = now.UTC()

	jobs, err := r.s.Jobs.All(ctx)
	if err != nil {
		return out, err
	}
	out.Jobs = len(jobs)
	for _, j := range jobs {
		if j.Enabled {
			out.JobsEnabled++
		}
		if j.Overdue {
			out.JobsOverdue++
		}
		if j.LastRun != nil && j.LastRun.Status == core.RunFailed {
			out.JobsFailing++
		}
	}

	out.RunsRunning, err = r.s.Runs.CountByStatus(ctx, core.RunRunning)
	if err != nil {
		return out, err
	}

	since := now.Add(-24 * time.Hour)
	if err := r.s.queryRow(ctx, `SELECT
			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status IN (?, ?) THEN 1 ELSE 0 END), 0)
		FROM runs WHERE queued_at >= ?`,
		string(core.RunSuccess), string(core.RunFailed), string(core.RunWarning), formatTime(since)).
		Scan(&out.Runs24hSuccess, &out.Runs24hFailed); err != nil {
		return out, fmt.Errorf("dashboard 24h counters: %w", err)
	}

	out.Artifacts, out.TotalBytes, err = r.s.Artifacts.Count(ctx)
	if err != nil {
		return out, err
	}

	out.Destinations, err = r.destinationUsage(ctx)
	if err != nil {
		return out, err
	}

	out.Daily, err = r.daily(ctx, now, dailyDays)
	if err != nil {
		return out, err
	}

	runs, _, err := r.s.Runs.List(ctx, RunFilter{Page: Page{Limit: recentRuns}})
	if err != nil {
		return out, err
	}
	out.RecentRuns = runs

	upcoming := []core.Job{}
	problems := []core.Job{}
	for _, j := range jobs {
		if j.Enabled && j.NextRunAt != nil {
			upcoming = append(upcoming, j)
		}
		if j.Overdue || (j.LastRun != nil && (j.LastRun.Status == core.RunFailed || j.LastRun.Status == core.RunWarning)) {
			problems = append(problems, j)
		}
	}
	sort.Slice(upcoming, func(i, k int) bool { return upcoming[i].NextRunAt.Before(*upcoming[k].NextRunAt) })
	if len(upcoming) > 10 {
		upcoming = upcoming[:10]
	}
	if len(problems) > 10 {
		problems = problems[:10]
	}
	out.Upcoming = upcoming
	out.ProblemJobs = problems
	return out, nil
}

func (r *StatsRepo) destinationUsage(ctx context.Context) ([]core.DestinationUsage, error) {
	dests, _, err := r.s.Destinations.List(ctx, DestinationFilter{Page: Page{Limit: MaxLimit}})
	if err != nil {
		return nil, err
	}
	out := make([]core.DestinationUsage, 0, len(dests))
	for _, d := range dests {
		out = append(out, core.DestinationUsage{
			DestinationID:   d.ID,
			DestinationName: d.Name,
			Kind:            d.Kind,
			Bytes:           d.UsedBytes,
			Artifacts:       d.ArtifactCount,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Bytes > out[j].Bytes })
	return out, nil
}

func (r *StatsRepo) daily(ctx context.Context, now time.Time, days int) ([]core.DailyStat, error) {
	if days <= 0 {
		days = 30
	}
	start := now.AddDate(0, 0, -(days - 1)).Truncate(24 * time.Hour)
	rows, err := r.s.query(ctx, `SELECT substr(queued_at, 1, 10) AS day,
			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status IN (?, ?) THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(bytes), 0)
		FROM runs WHERE queued_at >= ? GROUP BY day`,
		string(core.RunSuccess), string(core.RunFailed), string(core.RunWarning), formatTime(start))
	if err != nil {
		return nil, fmt.Errorf("daily stats: %w", err)
	}
	defer rows.Close()
	byDay := map[string]core.DailyStat{}
	for rows.Next() {
		var (
			day   sql.NullString
			stat  core.DailyStat
			bytes int64
		)
		if err := rows.Scan(&day, &stat.Success, &stat.Failed, &bytes); err != nil {
			return nil, fmt.Errorf("scan daily stats: %w", err)
		}
		stat.Bytes = bytes
		stat.Date = day.String
		byDay[stat.Date] = stat
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]core.DailyStat, 0, days)
	for i := 0; i < days; i++ {
		d := start.AddDate(0, 0, i).Format("2006-01-02")
		if stat, ok := byDay[d]; ok {
			stat.Date = d
			out = append(out, stat)
			continue
		}
		out = append(out, core.DailyStat{Date: d})
	}
	return out, nil
}
