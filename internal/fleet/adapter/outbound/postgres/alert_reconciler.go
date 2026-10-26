package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/application"
)

const (
	alertCheckLockKey = 7_301_001

	tryAlertCheckLockQuery = `SELECT pg_try_advisory_xact_lock($1)`

	// The reading must be received after the last activation (REG-07), and is
	// looked up in the shape the partial index answers; see the EXPLAIN test.
	openLowBatteryAlertsQuery = `
		INSERT INTO alerts (robot_id, kind, status, opened_at)
		SELECT r.id, 'LOW_BATTERY', 'OPEN', $1
		FROM robots r
		WHERE r.lifecycle = 'ACTIVE'
			AND r.battery_health = 'LOW'
			AND (
				SELECT o.received_at
				FROM telemetry_observations o
				WHERE o.robot_id = r.id AND o.outcome = 'APPLIED'
				ORDER BY o.observed_at DESC
				LIMIT 1
			) > r.activated_at
		ON CONFLICT (robot_id, kind) WHERE status IN ('OPEN', 'ACKNOWLEDGED') DO NOTHING`

	resolveRecoveredAlertsQuery = `
		UPDATE alerts a
		SET status = 'RESOLVED', ended_at = $1
		FROM robots r
		WHERE a.robot_id = r.id
			AND a.status IN ('OPEN', 'ACKNOWLEDGED')
			AND r.battery_health = 'NORMAL'`

	closeInactiveRobotsAlertsQuery = `
		UPDATE alerts a
		SET status = 'CLOSED', closure_reason = 'ROBOT_DEACTIVATED', ended_at = $1
		FROM robots r
		WHERE a.robot_id = r.id
			AND a.status IN ('OPEN', 'ACKNOWLEDGED')
			AND r.lifecycle = 'INACTIVE'`
)

type AlertReconciler struct {
	db *sql.DB
}

var _ application.AlertReconciler = (*AlertReconciler)(nil)

func NewAlertReconciler(db *sql.DB) *AlertReconciler {
	return &AlertReconciler{db: db}
}

func (reconciler *AlertReconciler) Reconcile(ctx context.Context, now time.Time) (application.AlertReconciliation, error) {
	tx, err := reconciler.db.BeginTx(ctx, nil)
	if err != nil {
		return application.AlertReconciliation{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var locked bool
	if err := tx.QueryRowContext(ctx, tryAlertCheckLockQuery, alertCheckLockKey).Scan(&locked); err != nil {
		return application.AlertReconciliation{}, fmt.Errorf("take alert check lock: %w", err)
	}
	if locked == false {
		return application.AlertReconciliation{Skipped: true}, nil
	}

	opened, err := execCount(ctx, tx, openLowBatteryAlertsQuery, now)
	if err != nil {
		return application.AlertReconciliation{}, fmt.Errorf("open alerts: %w", err)
	}

	resolved, err := execCount(ctx, tx, resolveRecoveredAlertsQuery, now)
	if err != nil {
		return application.AlertReconciliation{}, fmt.Errorf("resolve alerts: %w", err)
	}

	closed, err := execCount(ctx, tx, closeInactiveRobotsAlertsQuery, now)
	if err != nil {
		return application.AlertReconciliation{}, fmt.Errorf("close alerts: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return application.AlertReconciliation{}, fmt.Errorf("commit transaction: %w", err)
	}

	return application.AlertReconciliation{Opened: opened, Resolved: resolved, Closed: closed}, nil
}

func execCount(ctx context.Context, tx *sql.Tx, query string, args ...any) (int, error) {
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}

	count, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}

	return int(count), nil
}
