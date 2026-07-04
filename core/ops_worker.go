package core

import (
	"context"
	"time"
)

// StartOpsWorker runs the lightweight, Postgres-coordinated background jobs.
// Advisory locks inside each runner keep multiple Dublyobase instances from
// executing the same due job at once.
func StartOpsWorker(ctx context.Context, app *App, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runOpsTick(ctx, app)
		}
	}
}

func runOpsTick(ctx context.Context, app *App) {
	runCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	if err := RunDueCronJobs(runCtx, app.Pool, time.Now().UTC()); err != nil {
		app.Log.Warn("cron worker failed", "err", err)
	}
	storageCfg, err := EffectiveStorageConfig(runCtx, app.Pool, app.Config)
	if err != nil {
		app.Log.Warn("backup worker storage config failed", "err", err)
		return
	}
	if err := RunDueBackupJobs(runCtx, app.Pool, storageCfg, time.Now().UTC()); err != nil {
		app.Log.Warn("backup worker failed", "err", err)
	}
}
