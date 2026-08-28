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
	if err := RunDueWebhookDeliveries(runCtx, app.Pool, app.Config, time.Now().UTC()); err != nil {
		app.Log.Warn("webhook worker failed", "err", err)
	}
	// Sweep events whose creating request never marked them delivered. The age
	// threshold keeps this off events a live request is about to publish
	// normally, so the sweep only ever picks up genuine casualties.
	if app.OutboxPublish != nil {
		if err := SweepOutbox(runCtx, app.Pool, app.Log, app.OutboxPublish, 60*time.Second); err != nil {
			app.Log.Warn("outbox sweep failed", "err", err)
		}
	}
	logSettings, err := EffectiveLogSettings(runCtx, app.Pool)
	if err != nil {
		app.Log.Warn("log retention settings failed", "err", err)
	} else if deleted, err := PruneAuditLog(runCtx, app.Pool, logSettings.RetentionDays, logSettings.RetentionCount); err != nil {
		app.Log.Warn("log retention failed", "err", err)
	} else if deleted > 0 {
		app.Log.Info("log retention pruned audit rows", "deleted", deleted)
	}
	if logSettings.RetentionDays > 0 {
		if deleted, err := PruneRequestLogs(runCtx, app.Pool, logSettings.RetentionDays, logSettings.RetentionCount); err != nil {
			app.Log.Warn("request log retention failed", "err", err)
		} else if deleted > 0 {
			app.Log.Info("log retention pruned request rows", "deleted", deleted)
		}
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
