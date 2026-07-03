package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dublyo/dublyobase/apis"
	"github.com/dublyo/dublyobase/core"
	"github.com/spf13/cobra"
)

func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the API server and admin UI",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServe()
		},
	}
}

func runServe() error {
	// 1. Load + validate config. Fail loud on missing required vars.
	cfg, err := core.LoadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "configuration error:", err)
		os.Exit(1)
	}

	log := core.NewLogger(cfg)
	log.Info("starting dublyobase",
		"version", core.Version,
		"addr", cfg.Addr(),
		"app_url", cfg.AppURL,
		"storage", cfg.StorageType,
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 2. Connect to the external Postgres, retrying transient failures.
	pool, err := core.Connect(ctx, cfg, log)
	if err != nil {
		log.Error("could not connect to database", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	app := core.NewApp(cfg, pool, log)

	// 3. Run migrations on boot (idempotent) before serving traffic.
	if cfg.MigrateOnStart {
		if err := core.Migrate(ctx, pool, log); err != nil {
			log.Error("migration failed", "err", err)
			os.Exit(1)
		}
		if err := core.SeedAdmin(ctx, pool, cfg, log); err != nil {
			log.Error("admin seed failed", "err", err)
			os.Exit(1)
		}
	}
	app.SetReady(true)

	// 4. Serve; shut down gracefully on SIGINT/SIGTERM.
	srv := apis.NewServer(app)
	go func() {
		<-ctx.Done()
		log.Info("shutting down")
		shutCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	log.Info("listening", "addr", cfg.Addr())
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
