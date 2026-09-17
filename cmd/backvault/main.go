package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/arthurr0/backvault/internal/client"
)

var (
	flagConfig  string
	flagDataDir string
)

func main() {
	root := &cobra.Command{
		Use:           "backvault",
		Short:         "Backvault: every backup, accounted for",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&flagConfig, "config", "", "path to the configuration file")
	root.PersistentFlags().StringVar(&flagDataDir, "data-dir", "", "data directory")

	root.AddCommand(
		newServeCommand(),
		newCheckCommand(),
		newVersionCommand(),
		newRunCommand(),
		newJobsCommand(),
		newRunsCommand(),
		newRestoreCommand(),
		newPushCommand(),
		newUserCommand(),
		newTokenCommand(),
		newExportCommand(),
		newImportCommand(),
	)

	if err := root.Execute(); err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) {
			fmt.Fprintf(os.Stderr, "error: %s\n", apiErr.Error())
			os.Exit(exitCodeFor(apiErr.Status))
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func exitCodeFor(status int) int {
	switch {
	case status == 401 || status == 403:
		return 4
	case status == 409 || status == 423:
		return 5
	case status == 404:
		return 6
	case status == 0 || status >= 500:
		return 8
	default:
		return 1
	}
}
