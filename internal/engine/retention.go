package engine

import (
	"fmt"
	"sort"
	"time"

	"github.com/arthurr0/backvault/internal/core"
)

func Plan(artifacts []core.Artifact, r core.Retention, now time.Time) (keep, remove []core.Artifact) {
	items := make([]core.Artifact, len(artifacts))
	copy(items, artifacts)
	sort.SliceStable(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })

	keep = []core.Artifact{}
	remove = []core.Artifact{}
	if len(items) == 0 {
		return keep, remove
	}
	if r.IsZero() {
		return items, remove
	}

	kept := make([]bool, len(items))
	kept[0] = true

	if r.KeepLast > 0 {
		for i := 0; i < r.KeepLast && i < len(items); i++ {
			kept[i] = true
		}
	}

	applyBucket(items, kept, r.KeepHourly, func(t time.Time) string { return t.UTC().Format("2006-01-02T15") })
	applyBucket(items, kept, r.KeepDaily, func(t time.Time) string { return t.UTC().Format("2006-01-02") })
	applyBucket(items, kept, r.KeepWeekly, func(t time.Time) string {
		y, w := t.UTC().ISOWeek()
		return fmt.Sprintf("%04d-W%02d", y, w)
	})
	applyBucket(items, kept, r.KeepMonthly, func(t time.Time) string { return t.UTC().Format("2006-01") })
	applyBucket(items, kept, r.KeepYearly, func(t time.Time) string { return t.UTC().Format("2006") })

	hasKeepRule := r.KeepLast > 0 || r.KeepHourly > 0 || r.KeepDaily > 0 || r.KeepWeekly > 0 || r.KeepMonthly > 0 || r.KeepYearly > 0
	if !hasKeepRule && r.MaxAgeDays > 0 {
		cutoff := now.UTC().AddDate(0, 0, -r.MaxAgeDays)
		for i := range items {
			if !items[i].CreatedAt.UTC().Before(cutoff) {
				kept[i] = true
			}
		}
	}

	for i := range items {
		if kept[i] {
			keep = append(keep, items[i])
			continue
		}
		remove = append(remove, items[i])
	}
	return keep, remove
}

func applyBucket(items []core.Artifact, kept []bool, count int, key func(time.Time) string) {
	if count <= 0 {
		return
	}
	seen := map[string]bool{}
	taken := 0
	for i := range items {
		k := key(items[i].CreatedAt)
		if seen[k] {
			continue
		}
		seen[k] = true
		kept[i] = true
		taken++
		if taken >= count {
			return
		}
	}
}

func EffectiveRetention(job core.Job, settings core.Settings) core.Retention {
	if job.Retention.IsZero() {
		return settings.DefaultRetention
	}
	return job.Retention
}
