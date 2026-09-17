package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/arthurr0/backvault/internal/auth"
	"github.com/arthurr0/backvault/internal/config"
	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/store"
)

func openLocalStore(ctx context.Context) (*store.Store, config.Config, error) {
	cfg, err := config.Load(config.Flags{ConfigFile: flagConfig, DataDir: flagDataDir})
	if err != nil {
		return nil, cfg, err
	}
	if err := cfg.EnsureDirs(); err != nil {
		return nil, cfg, err
	}
	st, err := store.Open(ctx, cfg.DatabasePath())
	if err != nil {
		return nil, cfg, err
	}
	return st, cfg, nil
}

func newUserCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "user", Short: "Manage users directly in the database"}

	var (
		email    string
		name     string
		role     string
		password string
	)

	create := &cobra.Command{
		Use:   "create",
		Short: "Create a user",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(email) == "" {
				return fmt.Errorf("--email is required")
			}
			userRole := core.UserRole(strings.ToLower(strings.TrimSpace(role)))
			if userRole == "" {
				userRole = core.RoleAdmin
			}
			if userRole != core.RoleAdmin && userRole != core.RoleViewer {
				return fmt.Errorf("--role must be admin or viewer")
			}
			secret, err := resolvePassword(password)
			if err != nil {
				return err
			}
			st, _, err := openLocalStore(cmd.Context())
			if err != nil {
				return err
			}
			defer st.Close()
			manager := auth.NewManager(st)
			user, err := manager.CreateUser(cmd.Context(), core.User{Email: email, Name: name, Role: userRole}, secret)
			if err != nil {
				return err
			}
			fmt.Printf("created %s (%s)\n", user.Email, user.Role)
			return nil
		},
	}
	create.Flags().StringVar(&email, "email", "", "email address")
	create.Flags().StringVar(&name, "name", "", "display name")
	create.Flags().StringVar(&role, "role", "admin", "role: admin or viewer")
	create.Flags().StringVar(&password, "password", "", "password (prompted when omitted)")

	list := &cobra.Command{
		Use:   "list",
		Short: "List users",
		RunE: func(cmd *cobra.Command, args []string) error {
			st, _, err := openLocalStore(cmd.Context())
			if err != nil {
				return err
			}
			defer st.Close()
			users, err := st.Users.List(cmd.Context())
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "EMAIL\tNAME\tROLE\tCREATED\tLAST LOGIN")
			for _, u := range users {
				last := "-"
				if u.LastLoginAt != nil {
					last = u.LastLoginAt.Format(time.RFC3339)
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", u.Email, orDash(u.Name), u.Role, u.CreatedAt.Format(time.RFC3339), last)
			}
			return w.Flush()
		},
	}

	var (
		pwEmail    string
		pwPassword string
	)
	passwordCmd := &cobra.Command{
		Use:   "password",
		Short: "Set a user password",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(pwEmail) == "" {
				return fmt.Errorf("--email is required")
			}
			secret, err := resolvePassword(pwPassword)
			if err != nil {
				return err
			}
			st, _, err := openLocalStore(cmd.Context())
			if err != nil {
				return err
			}
			defer st.Close()
			rec, err := st.Users.GetByEmail(cmd.Context(), pwEmail)
			if err != nil {
				return err
			}
			manager := auth.NewManager(st)
			if err := manager.SetPassword(cmd.Context(), rec.ID, secret); err != nil {
				return err
			}
			if err := st.Sessions.DeleteForUser(cmd.Context(), rec.ID); err != nil {
				return err
			}
			fmt.Printf("password updated for %s\n", rec.Email)
			return nil
		},
	}
	passwordCmd.Flags().StringVar(&pwEmail, "email", "", "email address")
	passwordCmd.Flags().StringVar(&pwPassword, "password", "", "new password (prompted when omitted)")

	cmd.AddCommand(create, list, passwordCmd)
	return cmd
}

func newTokenCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "token", Short: "Manage API tokens directly in the database"}

	var (
		name    string
		scopes  []string
		jobs    []string
		expires string
	)
	create := &cobra.Command{
		Use:   "create",
		Short: "Create an API token",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(name) == "" {
				return fmt.Errorf("--name is required")
			}
			if err := auth.ValidateScopes(scopes); err != nil {
				return err
			}
			var expiresAt *time.Time
			if strings.TrimSpace(expires) != "" {
				t, err := time.Parse(time.RFC3339, expires)
				if err != nil {
					return fmt.Errorf("invalid --expires, want RFC3339: %w", err)
				}
				utc := t.UTC()
				expiresAt = &utc
			}
			st, _, err := openLocalStore(cmd.Context())
			if err != nil {
				return err
			}
			defer st.Close()
			manager := auth.NewManager(st)
			created, err := manager.CreateToken(cmd.Context(), core.APIToken{
				Name: name, Scopes: scopes, JobSlugs: jobs, ExpiresAt: expiresAt, CreatedBy: "cli",
			})
			if err != nil {
				return err
			}
			fmt.Println(created.Secret)
			fmt.Fprintf(os.Stderr, "token %s created with scopes %s\n", created.Token.Name, strings.Join(created.Token.Scopes, ","))
			return nil
		},
	}
	create.Flags().StringVar(&name, "name", "", "token name")
	create.Flags().StringSliceVar(&scopes, "scopes", []string{core.ScopeRead}, "scopes: admin, read, run, ingest")
	create.Flags().StringSliceVar(&jobs, "jobs", nil, "restrict the token to these job slugs")
	create.Flags().StringVar(&expires, "expires", "", "expiry as RFC3339")

	list := &cobra.Command{
		Use:   "list",
		Short: "List API tokens",
		RunE: func(cmd *cobra.Command, args []string) error {
			st, _, err := openLocalStore(cmd.Context())
			if err != nil {
				return err
			}
			defer st.Close()
			tokens, err := st.Tokens.List(cmd.Context())
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tNAME\tPREFIX\tSCOPES\tJOBS\tEXPIRES")
			for _, t := range tokens {
				exp := "-"
				if t.ExpiresAt != nil {
					exp = t.ExpiresAt.Format(time.RFC3339)
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", t.ID, t.Name, t.Prefix, strings.Join(t.Scopes, ","), joinOrDash(t.JobSlugs), exp)
			}
			return w.Flush()
		},
	}

	var deleteID string
	del := &cobra.Command{
		Use:   "delete",
		Short: "Delete an API token",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(deleteID) == "" {
				return fmt.Errorf("--id is required")
			}
			st, _, err := openLocalStore(cmd.Context())
			if err != nil {
				return err
			}
			defer st.Close()
			if err := st.Tokens.Delete(cmd.Context(), deleteID); err != nil {
				return err
			}
			fmt.Printf("deleted %s\n", deleteID)
			return nil
		},
	}
	del.Flags().StringVar(&deleteID, "id", "", "token id")

	cmd.AddCommand(create, list, del)
	return cmd
}

func resolvePassword(provided string) (string, error) {
	if provided != "" {
		if err := auth.ValidatePassword(provided); err != nil {
			return "", err
		}
		return provided, nil
	}
	if env := os.Getenv("BACKVAULT_PASSWORD"); env != "" {
		if err := auth.ValidatePassword(env); err != nil {
			return "", err
		}
		return env, nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		reader := bufio.NewReader(os.Stdin)
		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			return "", fmt.Errorf("read password from stdin: %w", err)
		}
		secret := strings.TrimRight(line, "\r\n")
		if err := auth.ValidatePassword(secret); err != nil {
			return "", err
		}
		return secret, nil
	}
	fmt.Fprint(os.Stderr, "Password: ")
	first, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	fmt.Fprint(os.Stderr, "Repeat password: ")
	second, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	if string(first) != string(second) {
		return "", fmt.Errorf("passwords do not match")
	}
	if err := auth.ValidatePassword(string(first)); err != nil {
		return "", err
	}
	return string(first), nil
}
