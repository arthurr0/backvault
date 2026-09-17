package engine

import (
	"testing"
	"time"

	"github.com/arthurr0/backvault/internal/core"
)

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t.UTC()
}

func artifacts(times ...string) []core.Artifact {
	out := make([]core.Artifact, 0, len(times))
	for _, ts := range times {
		out = append(out, core.Artifact{ID: ts, CreatedAt: at(ts), Status: core.ArtifactPresent})
	}
	return out
}

func ids(items []core.Artifact) []string {
	out := make([]string, 0, len(items))
	for _, a := range items {
		out = append(out, a.ID)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestPlan(t *testing.T) {
	now := at("2024-06-15T12:00:00Z")

	daily := artifacts(
		"2024-06-15T02:00:00Z",
		"2024-06-14T02:00:00Z",
		"2024-06-13T02:00:00Z",
		"2024-06-12T02:00:00Z",
		"2024-06-11T02:00:00Z",
	)

	cases := []struct {
		name      string
		items     []core.Artifact
		retention core.Retention
		keep      []string
		remove    []string
	}{
		{
			name:      "empty input",
			items:     nil,
			retention: core.Retention{KeepLast: 3},
			keep:      []string{},
			remove:    []string{},
		},
		{
			name:      "zero retention keeps everything",
			items:     daily,
			retention: core.Retention{},
			keep:      ids(daily),
			remove:    []string{},
		},
		{
			name:      "keep last two",
			items:     daily,
			retention: core.Retention{KeepLast: 2},
			keep:      []string{"2024-06-15T02:00:00Z", "2024-06-14T02:00:00Z"},
			remove:    []string{"2024-06-13T02:00:00Z", "2024-06-12T02:00:00Z", "2024-06-11T02:00:00Z"},
		},
		{
			name:      "keep last larger than input",
			items:     daily,
			retention: core.Retention{KeepLast: 99},
			keep:      ids(daily),
			remove:    []string{},
		},
		{
			name: "hourly buckets keep newest per hour",
			items: artifacts(
				"2024-06-15T10:50:00Z",
				"2024-06-15T10:10:00Z",
				"2024-06-15T09:50:00Z",
				"2024-06-15T08:50:00Z",
			),
			retention: core.Retention{KeepHourly: 2},
			keep:      []string{"2024-06-15T10:50:00Z", "2024-06-15T09:50:00Z"},
			remove:    []string{"2024-06-15T10:10:00Z", "2024-06-15T08:50:00Z"},
		},
		{
			name: "daily buckets keep newest per day",
			items: artifacts(
				"2024-06-15T22:00:00Z",
				"2024-06-15T02:00:00Z",
				"2024-06-14T22:00:00Z",
				"2024-06-14T02:00:00Z",
				"2024-06-13T02:00:00Z",
			),
			retention: core.Retention{KeepDaily: 2},
			keep:      []string{"2024-06-15T22:00:00Z", "2024-06-14T22:00:00Z"},
			remove:    []string{"2024-06-15T02:00:00Z", "2024-06-14T02:00:00Z", "2024-06-13T02:00:00Z"},
		},
		{
			name: "weekly buckets",
			items: artifacts(
				"2024-06-15T02:00:00Z",
				"2024-06-12T02:00:00Z",
				"2024-06-08T02:00:00Z",
				"2024-06-01T02:00:00Z",
			),
			retention: core.Retention{KeepWeekly: 2},
			keep:      []string{"2024-06-15T02:00:00Z", "2024-06-08T02:00:00Z"},
			remove:    []string{"2024-06-12T02:00:00Z", "2024-06-01T02:00:00Z"},
		},
		{
			name: "monthly buckets",
			items: artifacts(
				"2024-06-15T02:00:00Z",
				"2024-06-01T02:00:00Z",
				"2024-05-20T02:00:00Z",
				"2024-04-20T02:00:00Z",
			),
			retention: core.Retention{KeepMonthly: 2},
			keep:      []string{"2024-06-15T02:00:00Z", "2024-05-20T02:00:00Z"},
			remove:    []string{"2024-06-01T02:00:00Z", "2024-04-20T02:00:00Z"},
		},
		{
			name: "yearly buckets",
			items: artifacts(
				"2024-06-15T02:00:00Z",
				"2024-01-15T02:00:00Z",
				"2023-06-15T02:00:00Z",
				"2022-06-15T02:00:00Z",
			),
			retention: core.Retention{KeepYearly: 2},
			keep:      []string{"2024-06-15T02:00:00Z", "2023-06-15T02:00:00Z"},
			remove:    []string{"2024-01-15T02:00:00Z", "2022-06-15T02:00:00Z"},
		},
		{
			name: "max age only",
			items: artifacts(
				"2024-06-15T02:00:00Z",
				"2024-06-10T02:00:00Z",
				"2024-05-01T02:00:00Z",
			),
			retention: core.Retention{MaxAgeDays: 7},
			keep:      []string{"2024-06-15T02:00:00Z", "2024-06-10T02:00:00Z"},
			remove:    []string{"2024-05-01T02:00:00Z"},
		},
		{
			name: "max age never removes the newest",
			items: artifacts(
				"2023-01-01T02:00:00Z",
				"2022-01-01T02:00:00Z",
			),
			retention: core.Retention{MaxAgeDays: 7},
			keep:      []string{"2023-01-01T02:00:00Z"},
			remove:    []string{"2022-01-01T02:00:00Z"},
		},
		{
			name: "keep rules protect against max age",
			items: artifacts(
				"2024-06-15T02:00:00Z",
				"2024-01-01T02:00:00Z",
				"2023-01-01T02:00:00Z",
			),
			retention: core.Retention{KeepLast: 2, MaxAgeDays: 7},
			keep:      []string{"2024-06-15T02:00:00Z", "2024-01-01T02:00:00Z"},
			remove:    []string{"2023-01-01T02:00:00Z"},
		},
		{
			name: "combined rules union",
			items: artifacts(
				"2024-06-15T02:00:00Z",
				"2024-06-14T02:00:00Z",
				"2024-06-01T02:00:00Z",
				"2024-05-01T02:00:00Z",
				"2024-04-01T02:00:00Z",
			),
			retention: core.Retention{KeepLast: 1, KeepDaily: 2, KeepMonthly: 3},
			keep: []string{
				"2024-06-15T02:00:00Z",
				"2024-06-14T02:00:00Z",
				"2024-05-01T02:00:00Z",
				"2024-04-01T02:00:00Z",
			},
			remove: []string{"2024-06-01T02:00:00Z"},
		},
		{
			name:      "negative counts behave as zero but newest stays",
			items:     daily,
			retention: core.Retention{KeepLast: -3, KeepDaily: -1, MaxAgeDays: 1},
			keep:      []string{"2024-06-15T02:00:00Z"},
			remove:    []string{"2024-06-14T02:00:00Z", "2024-06-13T02:00:00Z", "2024-06-12T02:00:00Z", "2024-06-11T02:00:00Z"},
		},
		{
			name: "unsorted input is normalized",
			items: artifacts(
				"2024-06-11T02:00:00Z",
				"2024-06-15T02:00:00Z",
				"2024-06-13T02:00:00Z",
			),
			retention: core.Retention{KeepLast: 1},
			keep:      []string{"2024-06-15T02:00:00Z"},
			remove:    []string{"2024-06-13T02:00:00Z", "2024-06-11T02:00:00Z"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			keep, remove := Plan(tc.items, tc.retention, now)
			if !equal(ids(keep), tc.keep) {
				t.Errorf("keep = %v, want %v", ids(keep), tc.keep)
			}
			if !equal(ids(remove), tc.remove) {
				t.Errorf("remove = %v, want %v", ids(remove), tc.remove)
			}
			if len(keep)+len(remove) != len(tc.items) {
				t.Errorf("plan lost artifacts: %d + %d != %d", len(keep), len(remove), len(tc.items))
			}
		})
	}
}

func TestPlanNeverRemovesNewest(t *testing.T) {
	now := at("2030-01-01T00:00:00Z")
	items := artifacts("2024-06-15T02:00:00Z", "2024-06-14T02:00:00Z")
	keep, remove := Plan(items, core.Retention{MaxAgeDays: 1}, now)
	if len(keep) != 1 || keep[0].ID != "2024-06-15T02:00:00Z" {
		t.Fatalf("keep = %v", ids(keep))
	}
	if len(remove) != 1 {
		t.Fatalf("remove = %v", ids(remove))
	}
}

func TestEffectiveRetention(t *testing.T) {
	settings := core.Settings{DefaultRetention: core.Retention{KeepLast: 5}}
	if got := EffectiveRetention(core.Job{}, settings); got.KeepLast != 5 {
		t.Fatalf("expected default retention, got %+v", got)
	}
	if got := EffectiveRetention(core.Job{Retention: core.Retention{KeepLast: 2}}, settings); got.KeepLast != 2 {
		t.Fatalf("expected job retention, got %+v", got)
	}
}
