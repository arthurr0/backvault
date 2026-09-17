package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/notify"
)

type Notifier struct {
	e *Engine
}

func newNotifier(e *Engine) *Notifier {
	return &Notifier{e: e}
}

func (n *Notifier) start(ctx context.Context) {
	events, cancel := n.e.bus.Subscribe(256)
	n.e.wg.Add(1)
	go func() {
		defer n.e.wg.Done()
		defer cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-events:
				if !ok {
					return
				}
				if ev.Type != TopicRunUpdated {
					continue
				}
				run, isRun := ev.Data.(core.Run)
				if !isRun || !run.Status.Terminal() {
					continue
				}
				n.dispatchRun(ctx, run)
			}
		}
	}()
}

func eventTypeForRun(run core.Run) (string, notify.Severity, bool) {
	switch run.Kind {
	case core.RunPrune:
		if run.Status == core.RunSuccess {
			return core.EventPruneDone, notify.SeverityInfo, true
		}
	case core.RunRestore:
		switch run.Status {
		case core.RunSuccess:
			return core.EventRestoreDone, notify.SeverityInfo, true
		case core.RunFailed:
			return core.EventRestoreFailed, notify.SeverityError, true
		}
	case core.RunVerify:
		switch run.Status {
		case core.RunWarning:
			return core.EventArtifactMissing, notify.SeverityWarning, true
		case core.RunFailed:
			return core.EventRunFailed, notify.SeverityError, true
		case core.RunSuccess:
			return core.EventRunSuccess, notify.SeverityInfo, true
		}
	default:
		switch run.Status {
		case core.RunSuccess:
			return core.EventRunSuccess, notify.SeverityInfo, true
		case core.RunWarning:
			return core.EventRunWarning, notify.SeverityWarning, true
		case core.RunFailed:
			return core.EventRunFailed, notify.SeverityError, true
		}
	}
	return "", notify.SeverityInfo, false
}

func (n *Notifier) dispatchRun(ctx context.Context, run core.Run) {
	eventType, severity, ok := eventTypeForRun(run)
	if !ok {
		return
	}
	var job *core.Job
	if run.JobID != "" {
		j, err := n.e.store.Jobs.Get(ctx, run.JobID)
		if err == nil {
			job = &j
		}
	}
	settings := n.e.settings(ctx)
	title := fmt.Sprintf("%s: %s %s", settings.SiteName, runLabel(run), string(run.Status))
	message := buildRunMessage(run, job)

	fields := map[string]string{
		"job":      run.JobName,
		"kind":     string(run.Kind),
		"status":   string(run.Status),
		"duration": formatDuration(run.DurationMS),
		"size":     formatBytes(run.Bytes),
	}
	if run.Error != "" {
		fields["error"] = run.Error
	}
	if job != nil && len(job.DestinationNames) > 0 {
		fields["destinations"] = strings.Join(job.DestinationNames, ", ")
	}

	ev := notify.Event{
		Type:     eventType,
		Severity: severity,
		Title:    title,
		Message:  message,
		Time:     time.Now().UTC(),
		SiteName: settings.SiteName,
		BaseURL:  settings.BaseURL,
		Job:      job,
		Run:      &run,
		Fields:   fields,
		Link:     n.e.runLink(ctx, run.ID),
	}
	n.send(ctx, job, ev)
}

func (n *Notifier) dispatchJobEvent(ctx context.Context, job core.Job, eventType, title, message string) {
	settings := n.e.settings(ctx)
	severity := notify.SeverityWarning
	if eventType == core.EventRunSuccess || eventType == core.EventPruneDone || eventType == core.EventRestoreDone {
		severity = notify.SeverityInfo
	}
	jobCopy := job
	ev := notify.Event{
		Type:     eventType,
		Severity: severity,
		Title:    fmt.Sprintf("%s: %s", settings.SiteName, title),
		Message:  fmt.Sprintf("%s: %s", job.Name, message),
		Time:     time.Now().UTC(),
		SiteName: settings.SiteName,
		BaseURL:  settings.BaseURL,
		Job:      &jobCopy,
		Fields:   map[string]string{"job": job.Name, "schedule": job.Schedule},
	}
	if settings.BaseURL != "" {
		ev.Link = settings.BaseURL + "/jobs/" + job.Slug
	}
	n.send(ctx, &jobCopy, ev)
}

func (n *Notifier) send(ctx context.Context, job *core.Job, ev notify.Event) {
	channels, err := n.resolveChannels(ctx, job, ev.Type)
	if err != nil {
		n.e.log.Error("could not resolve notification channels", "error", err)
		return
	}
	if len(channels) == 0 {
		return
	}
	base := context.WithoutCancel(ctx)
	for _, c := range channels {
		channel := c
		n.e.wg.Add(1)
		go func() {
			defer n.e.wg.Done()
			n.deliver(base, channel, ev)
		}()
	}
}

func (n *Notifier) deliver(ctx context.Context, channel core.NotificationChannel, ev notify.Event) {
	driver, cfg, err := n.e.NotifierConfig(channel)
	if err != nil {
		n.recordSendResult(ctx, channel, err)
		return
	}
	sendCtx, cancel := context.WithTimeout(ctx, notifyTimeout)
	defer cancel()
	log := n.e.log.With("channel", channel.Name, "kind", channel.Kind)
	err = driver.Send(sendCtx, cfg, ev, log)
	n.recordSendResult(ctx, channel, err)
	if err != nil {
		n.e.log.Error("notification failed", "channel", channel.Name, "error", err)
		return
	}
	n.e.log.Info("notification sent", "channel", channel.Name, "event", ev.Type)
}

func (n *Notifier) recordSendResult(ctx context.Context, channel core.NotificationChannel, err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
		if observer := n.e.currentObserver(); observer != nil {
			observer.NotificationFailure(channel.Name)
		}
	}
	if serr := n.e.store.Channels.SetSendResult(ctx, channel.ID, time.Now().UTC(), msg); serr != nil {
		n.e.log.Error("could not store notification result", "channel", channel.Name, "error", serr)
	}
}

func (n *Notifier) resolveChannels(ctx context.Context, job *core.Job, eventType string) ([]core.NotificationChannel, error) {
	settings := n.e.settings(ctx)
	wanted := settings.DefaultNotifyOn
	if job != nil && len(job.NotifyOn) > 0 {
		wanted = job.NotifyOn
	}
	if !contains(wanted, eventType) {
		return nil, nil
	}

	all, err := n.e.store.Channels.List(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[string]core.NotificationChannel{}
	for _, c := range all {
		byID[c.ID] = c
	}

	var candidates []core.NotificationChannel
	if job != nil && len(job.NotificationChannelIDs) > 0 {
		for _, id := range job.NotificationChannelIDs {
			if c, ok := byID[id]; ok {
				candidates = append(candidates, c)
			}
		}
	} else {
		candidates = all
	}

	out := make([]core.NotificationChannel, 0, len(candidates))
	for _, c := range candidates {
		if !c.Enabled {
			continue
		}
		if len(c.Events) > 0 && !contains(c.Events, eventType) {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func runLabel(run core.Run) string {
	if run.JobName != "" {
		return run.JobName
	}
	return string(run.Kind)
}

func buildRunMessage(run core.Run, job *core.Job) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s run of %s finished with status %s", run.Kind, runLabel(run), run.Status))
	if run.DurationMS > 0 {
		sb.WriteString(" after ")
		sb.WriteString(formatDuration(run.DurationMS))
	}
	if run.Bytes > 0 {
		sb.WriteString(", ")
		sb.WriteString(formatBytes(run.Bytes))
	}
	sb.WriteString(".")
	if job != nil && len(job.DestinationNames) > 0 {
		sb.WriteString(" Destinations: ")
		sb.WriteString(strings.Join(job.DestinationNames, ", "))
		sb.WriteString(".")
	}
	if run.Error != "" {
		sb.WriteString(" Error: ")
		sb.WriteString(run.Error)
	}
	return sb.String()
}

func formatDuration(ms int64) string {
	d := time.Duration(ms) * time.Millisecond
	if d < time.Second {
		return fmt.Sprintf("%dms", ms)
	}
	return d.Round(time.Second).String()
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
