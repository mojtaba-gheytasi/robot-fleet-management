package alertcheck

import (
	"context"
	"log/slog"
	"time"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/application"
)

type ReconcileAlertsUseCase interface {
	Execute(ctx context.Context) (application.AlertReconciliation, error)
}

type AlertCheck struct {
	reconcile ReconcileAlertsUseCase
	interval  time.Duration
	logger    *slog.Logger
}

func New(reconcile ReconcileAlertsUseCase, interval time.Duration, logger *slog.Logger) *AlertCheck {
	return &AlertCheck{reconcile: reconcile, interval: interval, logger: logger}
}

// Run checks once straight away, then on every tick, until ctx is cancelled.
func (check *AlertCheck) Run(ctx context.Context) {
	ticker := time.NewTicker(check.interval)
	defer ticker.Stop()

	for {
		check.runOnce(ctx)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (check *AlertCheck) runOnce(ctx context.Context) {
	result, err := check.reconcile.Execute(ctx)
	if err != nil {
		if ctx.Err() == nil {
			check.logger.Error("alert check failed", "error", err)
		}
		return
	}

	level := slog.LevelDebug
	if result.Opened > 0 || result.Resolved > 0 || result.Closed > 0 {
		level = slog.LevelInfo
	}

	check.logger.Log(ctx, level, "alert check ran",
		"skipped", result.Skipped,
		"opened", result.Opened,
		"resolved", result.Resolved,
		"closed", result.Closed,
	)
}
