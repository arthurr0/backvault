package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/arthurr0/backvault/internal/auth"
	"github.com/arthurr0/backvault/internal/config"
	"github.com/arthurr0/backvault/internal/drivers"
	"github.com/arthurr0/backvault/internal/engine"
	"github.com/arthurr0/backvault/internal/secrets"
	"github.com/arthurr0/backvault/internal/server"
	"github.com/arthurr0/backvault/internal/store"
	"github.com/arthurr0/backvault/internal/version"
	"github.com/arthurr0/backvault/web"
)

func newServeCommand() *cobra.Command {
	var (
		listen    string
		workDir   string
		baseURL   string
		logLevel  string
		logFormat string
	)
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the Backvault server",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(config.Flags{
				ConfigFile: flagConfig,
				Listen:     listen,
				DataDir:    flagDataDir,
				WorkDir:    workDir,
				BaseURL:    baseURL,
				LogLevel:   logLevel,
				LogFormat:  logFormat,
			})
			if err != nil {
				return err
			}
			return serve(cmd.Context(), cfg)
		},
	}
	cmd.Flags().StringVar(&listen, "listen", "", "address to listen on")
	cmd.Flags().StringVar(&workDir, "work-dir", "", "spool directory for runs")
	cmd.Flags().StringVar(&baseURL, "base-url", "", "public base URL")
	cmd.Flags().StringVar(&logLevel, "log-level", "", "log level: debug, info, warn, error")
	cmd.Flags().StringVar(&logFormat, "log-format", "", "log format: text or json")
	return cmd
}

func newLogger(cfg config.Config) *slog.Logger {
	var level slog.Level
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: level}
	if cfg.LogFormat == "json" {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}

func serve(ctx context.Context, cfg config.Config) error {
	if ctx == nil {
		ctx = context.Background()
	}
	log := newLogger(cfg)
	slog.SetDefault(log)

	if err := cfg.EnsureDirs(); err != nil {
		return err
	}

	key, err := secrets.LoadKey(cfg.MasterKey, cfg.MasterKeyFile)
	if err != nil {
		return err
	}
	cipher, err := secrets.New(key)
	if err != nil {
		return err
	}

	st, err := store.Open(ctx, cfg.DatabasePath())
	if err != nil {
		return err
	}
	defer st.Close()

	drivers.Register()

	manager := auth.NewManager(st)
	created, err := manager.BootstrapAdmin(ctx, cfg.AdminEmail, cfg.AdminPassword)
	if err != nil {
		return fmt.Errorf("bootstrap admin: %w", err)
	}
	if created {
		log.Info("bootstrap administrator created", "email", cfg.AdminEmail)
	}

	eng := engine.New(engine.Deps{
		Store:   st,
		Secrets: cipher,
		WorkDir: cfg.WorkDir,
		Logger:  log,
		BaseURL: cfg.BaseURL,
	})

	srv, err := server.New(server.Options{
		Store:    st,
		Engine:   eng,
		Auth:     manager,
		Secrets:  cipher,
		Config:   cfg,
		Logger:   log,
		StaticFS: web.FS(),
	})
	if err != nil {
		return err
	}
	eng.SetObserver(srv.Metrics())

	if err := eng.Start(ctx); err != nil {
		return fmt.Errorf("start engine: %w", err)
	}

	httpServer := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 20 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	shutdownCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		info := version.Info()
		log.Info("backvault is listening",
			"address", cfg.Listen, "version", info.Version, "dataDir", cfg.DataDir, "workDir", cfg.WorkDir)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("http server: %w", err)
		}
	case <-shutdownCtx.Done():
		log.Info("shutting down")
	}

	httpCtx, cancelHTTP := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelHTTP()
	if err := httpServer.Shutdown(httpCtx); err != nil {
		log.Warn("http shutdown was not clean", "error", err)
	}

	engineCtx, cancelEngine := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelEngine()
	if err := eng.Stop(engineCtx); err != nil {
		log.Warn("engine shutdown was not clean", "error", err)
	}
	log.Info("goodbye")
	return nil
}
