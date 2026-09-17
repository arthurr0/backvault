package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/arthurr0/backvault/internal/version"
)

func newVersionCommand() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			info := version.Info()
			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(info)
			}
			fmt.Printf("backvault %s\n", info.Version)
			fmt.Printf("commit:   %s\n", info.Commit)
			fmt.Printf("built:    %s\n", info.BuildDate)
			fmt.Printf("go:       %s\n", info.GoVersion)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}
