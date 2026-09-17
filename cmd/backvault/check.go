package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/arthurr0/backvault/internal/config"
	"github.com/arthurr0/backvault/internal/drivers"
	"github.com/arthurr0/backvault/internal/engine"
	"github.com/arthurr0/backvault/internal/secrets"
)

func newCheckCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Report available external tools and configuration problems",
		RunE: func(cmd *cobra.Command, args []string) error {
			drivers.Register()

			cfg, cfgErr := config.Load(config.Flags{ConfigFile: flagConfig, DataDir: flagDataDir})

			fmt.Println("Configuration")
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			for _, row := range cfg.Summary() {
				fmt.Fprintf(w, "  %s\t%s\n", row[0], row[1])
			}
			_ = w.Flush()
			fmt.Println()

			problems := 0
			if cfgErr != nil {
				problems++
				fmt.Printf("Configuration problem: %v\n\n", cfgErr)
			} else {
				if err := cfg.EnsureDirs(); err != nil {
					problems++
					fmt.Printf("Directory problem: %v\n\n", err)
				}
				if _, err := secrets.LoadKey(cfg.MasterKey, cfg.MasterKeyFile); err != nil {
					problems++
					fmt.Printf("Master key problem: %v\n\n", err)
				}
			}

			fmt.Println("External tools")
			tools := engine.Tools(cmd.Context())
			tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "  TOOL\tSTATUS\tUSED BY\tPATH\tVERSION")
			missing := 0
			for _, tool := range tools {
				status := "missing"
				if tool.Available {
					status = "ok"
				} else {
					missing++
				}
				fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n", tool.Name, status, joinOrDash(tool.UsedBy), orDash(tool.Path), orDash(tool.Version))
			}
			_ = tw.Flush()
			fmt.Println()
			fmt.Printf("%d tool(s) available, %d missing, %d configuration problem(s)\n", len(tools)-missing, missing, problems)
			if problems > 0 {
				return fmt.Errorf("%d configuration problem(s) found", problems)
			}
			return nil
		},
	}
}

func orDash(v string) string {
	if v == "" {
		return "-"
	}
	return v
}

func joinOrDash(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	out := values[0]
	for _, v := range values[1:] {
		out += "," + v
	}
	return out
}
