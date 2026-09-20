package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

func newHostsCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "hosts", Short: "Work with SSH hosts through the server API"}

	var query string
	list := &cobra.Command{
		Use:   "list",
		Short: "List hosts",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := apiClient()
			if err != nil {
				return err
			}
			hosts, err := c.Hosts(cmd.Context(), query)
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tADDRESS\tUSER\tAUTH\tSOURCES\tLAST TEST\tTOOLS")
			for _, h := range hosts {
				test := "-"
				if h.LastTestAt != nil {
					state := "failed"
					if h.LastTestOK != nil && *h.LastTestOK {
						state = "ok"
					}
					test = state + " " + h.LastTestAt.Format(time.RFC3339)
				}
				address := fmt.Sprintf("%s:%d", h.Address, h.Port)
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\t%s\n",
					h.Name, address, orDash(h.User), orDash(string(h.Auth)), h.SourceCount, test, orDash(strings.Join(h.Tools, " ")))
			}
			return w.Flush()
		},
	}
	list.Flags().StringVar(&query, "q", "", "filter by name, description or address")

	show := &cobra.Command{
		Use:   "show <name-or-id>",
		Short: "Show one host as JSON",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := apiClient()
			if err != nil {
				return err
			}
			host, err := c.Host(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return printJSON(host)
		},
	}

	test := &cobra.Command{
		Use:   "test <name-or-id>",
		Short: "Test the SSH connection to a host",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := apiClient()
			if err != nil {
				return err
			}
			host, err := c.Host(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			result, err := c.TestHost(cmd.Context(), host.ID)
			if err != nil {
				return err
			}
			fmt.Printf("ok: %t\n", result.OK)
			fmt.Printf("message: %s\n", result.Message)
			if result.OS != "" {
				fmt.Printf("os: %s\n", result.OS)
			}
			if len(result.Tools) > 0 {
				fmt.Printf("tools: %s\n", strings.Join(result.Tools, " "))
			}
			fmt.Printf("duration: %dms\n", result.DurationMS)
			if !result.OK {
				return fmt.Errorf("host %s is not reachable", host.Name)
			}
			return nil
		},
	}

	cmd.AddCommand(list, show, test)
	return cmd
}
