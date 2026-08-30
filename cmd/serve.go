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
	// 1. Load + validate config. Fail loud on missing/malformed vars.
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
		if errors.Is(err, context.Canceled) {
			log.Info("shutdown requested during startup")
			return nil
		}
		log.Error("could not connect to database", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	core.SetCronAllowPrivateTargets(cfg.CronAllowPrivateTargets)
	app := core.NewApp(cfg, pool, log)

	// 3. Migrate + seed before binding the public listener. This avoids any
	// setup-window ambiguity during cold boot and guarantees the first visible
	// admin state is the seeded state.
	if cfg.MigrateOnStart {
		if err := core.Migrate(ctx, pool, log); err != nil {
			log.Error("migration failed", "err", err)
			os.Exit(1)
		}
	}
	// A restored backup carries every row but none of the cluster's roles, so
	// the policies naming them fail to create and the instance comes up holding
	// its data with no security attached to it. Repair that before serving.
	if cfg.MigrateOnStart {
		if err := core.ReconcileProjectSecurity(ctx, pool, log); err != nil {
			log.Error("project security reconciliation failed", "err", err)
			os.Exit(1)
		}
	}
	if err := core.SeedAdmin(ctx, pool, cfg, log); err != nil {
		// With migrations disabled the schema may not exist yet; that must
		// not take the server down, but it must be visible.
		if cfg.MigrateOnStart {
			log.Error("admin seed failed", "err", err)
			os.Exit(1)
		}
		log.Warn("admin seed skipped", "err", err)
	}
	app.SetReady(true)
	go core.StartOpsWorker(ctx, app, time.Minute)
	log.Info("ready")

	// 4. Start serving after the app is ready.
	srv := apis.NewServer(app)
	serveErr := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.Addr())
		serveErr <- srv.ListenAndServe()
	}()

	// 5. Wait for shutdown or a server error; drain connections fully before
	//    closing the pool (Shutdown must be awaited, not fired-and-forgotten).
	select {
	case <-ctx.Done():
		log.Info("shutting down")
		shutCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			log.Warn("shutdown incomplete", "err", err)
		}
		<-serveErr // ListenAndServe has returned ErrServerClosed
		// Requests are queued for logging rather than written inline, so the
		// last few would be lost if the process left without draining them.
		app.RequestLogs.Close()
		return nil
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
