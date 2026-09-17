package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/arthurr0/backvault/internal/client"
	"github.com/arthurr0/backvault/internal/core"
)

func apiClient() (*client.Client, error) {
	return client.New()
}

func newRunCommand() *cobra.Command {
	var wait bool
	cmd := &cobra.Command{
		Use:   "run <job-slug>",
		Short: "Queue a backup run through the server API",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := apiClient()
			if err != nil {
				return err
			}
			var result struct {
				Run core.Run `json:"run"`
			}
			if err := c.Do(cmd.Context(), "POST", "/jobs/"+args[0]+"/run", map[string]any{}, &result); err != nil {
				return err
			}
			fmt.Println(result.Run.ID)
			if !wait {
				return nil
			}
			run, err := waitForRun(cmd.Context(), c, result.Run.ID)
			if err != nil {
				return err
			}
			fmt.Printf("status: %s\n", run.Status)
			if run.Error != "" {
				fmt.Printf("error: %s\n", run.Error)
			}
			if run.Status == core.RunFailed {
				return fmt.Errorf("run %s failed", run.ID)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&wait, "wait", false, "wait until the run finishes")
	return cmd
}

func waitForRun(ctx context.Context, c *client.Client, runID string) (core.Run, error) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		var run core.Run
		if err := c.Do(ctx, "GET", "/runs/"+runID, nil, &run); err != nil {
			return run, err
		}
		if run.Status.Terminal() {
			return run, nil
		}
		select {
		case <-ctx.Done():
			return run, ctx.Err()
		case <-ticker.C:
		}
	}
}

func newJobsCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "jobs", Short: "Work with jobs through the server API"}

	list := &cobra.Command{
		Use:   "list",
		Short: "List jobs",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := apiClient()
			if err != nil {
				return err
			}
			var out client.List[core.Job]
			if err := c.Do(cmd.Context(), "GET", "/jobs?limit=500", nil, &out); err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "SLUG\tNAME\tENABLED\tSCHEDULE\tLAST RUN\tNEXT RUN")
			for _, job := range out.Items {
				last := "-"
				if job.LastRun != nil {
					last = string(job.LastRun.Status)
				}
				next := "-"
				if job.NextRunAt != nil {
					next = job.NextRunAt.Format(time.RFC3339)
				}
				fmt.Fprintf(w, "%s\t%s\t%t\t%s\t%s\t%s\n", job.Slug, job.Name, job.Enabled, orDash(job.Schedule), last, next)
			}
			return w.Flush()
		},
	}

	show := &cobra.Command{
		Use:   "show <job-slug>",
		Short: "Show one job as JSON",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := apiClient()
			if err != nil {
				return err
			}
			var job core.Job
			if err := c.Do(cmd.Context(), "GET", "/jobs/"+args[0], nil, &job); err != nil {
				return err
			}
			return printJSON(job)
		},
	}

	toggle := func(use, short, action string) *cobra.Command {
		return &cobra.Command{
			Use:   use + " <job-slug>",
			Short: short,
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := apiClient()
				if err != nil {
					return err
				}
				var job core.Job
				if err := c.Do(cmd.Context(), "POST", "/jobs/"+args[0]+"/"+action, map[string]any{}, &job); err != nil {
					return err
				}
				fmt.Printf("%s enabled=%t\n", job.Slug, job.Enabled)
				return nil
			},
		}
	}

	cmd.AddCommand(list, show, toggle("enable", "Enable a job", "enable"), toggle("disable", "Disable a job", "disable"))
	return cmd
}

func newRunsCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "runs", Short: "Work with runs through the server API"}

	var (
		job    string
		status string
		limit  int
	)
	list := &cobra.Command{
		Use:   "list",
		Short: "List runs",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := apiClient()
			if err != nil {
				return err
			}
			query := "/runs?limit=" + strconv.Itoa(limit)
			if job != "" {
				query += "&job=" + job
			}
			if status != "" {
				query += "&status=" + status
			}
			var out client.List[core.Run]
			if err := c.Do(cmd.Context(), "GET", query, nil, &out); err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tJOB\tKIND\tSTATUS\tQUEUED\tDURATION\tBYTES")
			for _, run := range out.Items {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%dms\t%d\n",
					run.ID, orDash(run.JobSlug), run.Kind, run.Status, run.QueuedAt.Format(time.RFC3339), run.DurationMS, run.Bytes)
			}
			return w.Flush()
		},
	}
	list.Flags().StringVar(&job, "job", "", "filter by job slug")
	list.Flags().StringVar(&status, "status", "", "filter by status")
	list.Flags().IntVar(&limit, "limit", 50, "maximum number of runs")

	var follow bool
	logCmd := &cobra.Command{
		Use:   "log <run-id>",
		Short: "Print the log of a run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := apiClient()
			if err != nil {
				return err
			}
			offset := 0
			for {
				resp, err := c.Raw(cmd.Context(), "GET", "/runs/"+args[0]+"/log?offset="+strconv.Itoa(offset))
				if err != nil {
					return err
				}
				n, err := io.Copy(os.Stdout, resp.Body)
				resp.Body.Close()
				if err != nil {
					return fmt.Errorf("read log: %w", err)
				}
				offset += int(n)
				if !follow {
					return nil
				}
				var run core.Run
				if err := c.Do(cmd.Context(), "GET", "/runs/"+args[0], nil, &run); err != nil {
					return err
				}
				if run.Status.Terminal() {
					return nil
				}
				select {
				case <-cmd.Context().Done():
					return cmd.Context().Err()
				case <-time.After(time.Second):
				}
			}
		},
	}
	logCmd.Flags().BoolVarP(&follow, "follow", "f", false, "keep streaming until the run finishes")

	cmd.AddCommand(list, logCmd)
	return cmd
}

func newRestoreCommand() *cobra.Command {
	var (
		to         string
		raw        bool
		passphrase string
	)
	cmd := &cobra.Command{
		Use:   "restore <artifact-id>",
		Short: "Download an artifact to a local path",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(to) == "" {
				return fmt.Errorf("--to is required")
			}
			c, err := apiClient()
			if err != nil {
				return err
			}
			path := "/artifacts/" + args[0] + "/download"
			params := []string{}
			if raw {
				params = append(params, "raw=1")
			}
			if passphrase != "" {
				params = append(params, "passphrase="+passphrase)
			}
			if len(params) > 0 {
				path += "?" + strings.Join(params, "&")
			}
			resp, err := c.Raw(cmd.Context(), "GET", path)
			if err != nil {
				return err
			}
			defer resp.Body.Close()

			target := to
			if info, err := os.Stat(target); err == nil && info.IsDir() {
				target = target + string(os.PathSeparator) + filenameFromResponse(resp.Header.Get("Content-Disposition"), args[0])
			}
			tmp := target + ".partial"
			f, err := os.Create(tmp)
			if err != nil {
				return fmt.Errorf("create %s: %w", tmp, err)
			}
			written, err := io.Copy(f, resp.Body)
			if closeErr := f.Close(); err == nil {
				err = closeErr
			}
			if err != nil {
				_ = os.Remove(tmp)
				return fmt.Errorf("write %s: %w", tmp, err)
			}
			if err := os.Rename(tmp, target); err != nil {
				_ = os.Remove(tmp)
				return fmt.Errorf("install %s: %w", target, err)
			}
			fmt.Printf("%s (%d bytes)\n", target, written)
			return nil
		},
	}
	cmd.Flags().StringVar(&to, "to", "", "local path to write to")
	cmd.Flags().BoolVar(&raw, "raw", false, "do not decompress or decrypt")
	cmd.Flags().StringVar(&passphrase, "passphrase", "", "decryption passphrase override")
	return cmd
}

func filenameFromResponse(disposition, fallback string) string {
	idx := strings.Index(disposition, "filename=")
	if idx < 0 {
		return fallback
	}
	name := strings.Trim(disposition[idx+len("filename="):], "\"")
	if name == "" {
		return fallback
	}
	return name
}

func newExportCommand() *cobra.Command {
	var (
		includeSecrets bool
		out            string
	)
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export sources, destinations, jobs and channels as YAML",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := apiClient()
			if err != nil {
				return err
			}
			path := "/export"
			if includeSecrets {
				path += "?includeSecrets=1"
			}
			resp, err := c.Raw(cmd.Context(), "GET", path)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			writer := io.Writer(os.Stdout)
			if out != "" {
				f, err := os.Create(out)
				if err != nil {
					return fmt.Errorf("create %s: %w", out, err)
				}
				defer f.Close()
				writer = f
			}
			if _, err := io.Copy(writer, resp.Body); err != nil {
				return fmt.Errorf("write export: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&includeSecrets, "include-secrets", false, "include secret values in the export")
	cmd.Flags().StringVar(&out, "out", "", "write to a file instead of stdout")
	return cmd
}

func newImportCommand() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "import <file>",
		Short: "Import a YAML export",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := apiClient()
			if err != nil {
				return err
			}
			body, err := os.ReadFile(args[0])
			if err != nil {
				return fmt.Errorf("read %s: %w", args[0], err)
			}
			path := "/import"
			if dryRun {
				path += "?dryRun=1"
			}
			var result struct {
				DryRun  bool `json:"dryRun"`
				Total   int  `json:"total"`
				Changes []struct {
					Kind   string `json:"kind"`
					Name   string `json:"name"`
					Action string `json:"action"`
					Note   string `json:"note"`
				} `json:"changes"`
			}
			if err := c.Stream(cmd.Context(), "POST", path, strings.NewReader(string(body)), map[string]string{"Content-Type": "application/yaml"}, int64(len(body)), &result); err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "KIND\tNAME\tACTION\tNOTE")
			for _, change := range result.Changes {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", change.Kind, change.Name, change.Action, orDash(change.Note))
			}
			if err := w.Flush(); err != nil {
				return err
			}
			if result.DryRun {
				fmt.Printf("dry run: %d change(s) planned\n", result.Total)
				return nil
			}
			fmt.Printf("%d change(s) applied\n", result.Total)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show the plan without applying it")
	return cmd
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
