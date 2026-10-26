//go:build integration

package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/adapter/outbound/postgres"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

var checkedAt = time.Date(2026, time.October, 25, 15, 0, 0, 0, time.UTC)

func TestAlertReconcilerOpensOneAlertForALowRobot(t *testing.T) {
	ctx := context.Background()
	db := startPostgres(t)
	robotWithBatteryHealth(t, db, "robot-101", domain.BatteryHealthLow)
	reconciler := postgres.NewAlertReconciler(db)

	first, err := reconciler.Reconcile(ctx, checkedAt)
	require.NoError(t, err)
	assert.Equal(t, 1, first.Opened)

	second, err := reconciler.Reconcile(ctx, checkedAt.Add(5*time.Second))
	require.NoError(t, err)
	assert.Equal(t, 0, second.Opened, "no second alert while one is unresolved")

	alerts := storedAlerts(t, db)
	require.Len(t, alerts, 1)
	assert.Equal(t, "OPEN", alerts[0].status)
	assert.True(t, alerts[0].openedAt.Equal(checkedAt))
	assert.False(t, alerts[0].endedAt.Valid)
}

func TestAlertReconcilerResolvesOnRecoveryAndKeepsHistory(t *testing.T) {
	ctx := context.Background()
	db := startPostgres(t)
	robotWithBatteryHealth(t, db, "robot-101", domain.BatteryHealthLow)
	reconciler := postgres.NewAlertReconciler(db)
	_, err := reconciler.Reconcile(ctx, checkedAt)
	require.NoError(t, err)

	setBatteryHealth(t, db, "robot-101", domain.BatteryHealthNormal)
	recovered, err := reconciler.Reconcile(ctx, checkedAt.Add(5*time.Second))
	require.NoError(t, err)
	assert.Equal(t, 1, recovered.Resolved)

	setBatteryHealth(t, db, "robot-101", domain.BatteryHealthLow)
	lowAgain, err := reconciler.Reconcile(ctx, checkedAt.Add(10*time.Second))
	require.NoError(t, err)
	assert.Equal(t, 1, lowAgain.Opened)

	alerts := storedAlerts(t, db)
	require.Len(t, alerts, 2)
	assert.Equal(t, "RESOLVED", alerts[0].status)
	assert.True(t, alerts[0].endedAt.Time.Equal(checkedAt.Add(5*time.Second)))
	assert.Equal(t, "OPEN", alerts[1].status)
}

func TestAlertReconcilerLeavesHealthyAndUnreportedRobotsAlone(t *testing.T) {
	ctx := context.Background()
	db := startPostgres(t)
	robotWithBatteryHealth(t, db, "robot-101", domain.BatteryHealthUnknown)
	robotWithBatteryHealth(t, db, "robot-102", domain.BatteryHealthNormal)

	result, err := postgres.NewAlertReconciler(db).Reconcile(ctx, checkedAt)
	require.NoError(t, err)

	assert.Equal(t, 0, result.Opened)
	assert.Empty(t, storedAlerts(t, db))
}

func TestAlertReconcilerSkipsWhileAnotherCheckHoldsTheLock(t *testing.T) {
	ctx := context.Background()
	db := startPostgres(t)
	robotWithBatteryHealth(t, db, "robot-101", domain.BatteryHealthLow)

	other, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = other.Rollback() })
	var locked bool
	require.NoError(t, other.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(7301001)`).Scan(&locked))
	require.True(t, locked)

	result, err := postgres.NewAlertReconciler(db).Reconcile(ctx, checkedAt)
	require.NoError(t, err)

	assert.True(t, result.Skipped)
	assert.Empty(t, storedAlerts(t, db))
}

func TestOverlappingAlertChecksOpenOnlyOneAlert(t *testing.T) {
	ctx := context.Background()
	db := startPostgres(t)
	robotWithBatteryHealth(t, db, "robot-101", domain.BatteryHealthLow)
	reconciler := postgres.NewAlertReconciler(db)

	var opened atomic.Int32
	var wait sync.WaitGroup
	for range 10 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, err := reconciler.Reconcile(ctx, checkedAt)
			if assert.NoError(t, err) {
				opened.Add(int32(result.Opened))
			}
		}()
	}
	wait.Wait()

	assert.Equal(t, int32(1), opened.Load())
	assert.Len(t, storedAlerts(t, db), 1)
}

func TestTheDatabaseRejectsASecondUnresolvedAlert(t *testing.T) {
	ctx := context.Background()
	db := startPostgres(t)
	robotWithBatteryHealth(t, db, "robot-101", domain.BatteryHealthLow)
	insert := `INSERT INTO alerts (robot_id, kind, status, opened_at) VALUES ('robot-101', 'LOW_BATTERY', $1, now())`

	_, err := db.ExecContext(ctx, insert, "OPEN")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, insert, "ACKNOWLEDGED")

	var postgresError *pgconn.PgError
	require.True(t, errors.As(err, &postgresError), "want a PostgreSQL error, got %v", err)
	assert.Equal(t, "23505", postgresError.Code)
	assert.Equal(t, "alerts_one_unresolved_idx", postgresError.ConstraintName)
}

func robotWithBatteryHealth(t *testing.T, db *sql.DB, id domain.RobotID, health domain.BatteryHealth) {
	t.Helper()

	require.NoError(t, postgres.NewRobotRepository(db).Create(context.Background(), newRobot(t, id, "site-north")))
	setBatteryHealth(t, db, id, health)
}

func setBatteryHealth(t *testing.T, db *sql.DB, id domain.RobotID, health domain.BatteryHealth) {
	t.Helper()

	_, err := db.Exec(`UPDATE robots SET battery_health = $2 WHERE id = $1`, string(id), string(health))
	require.NoError(t, err)
}

type alertRow struct {
	status   string
	openedAt time.Time
	endedAt  sql.NullTime
}

func storedAlerts(t *testing.T, db *sql.DB) []alertRow {
	t.Helper()

	rows, err := db.Query(`SELECT status, opened_at, ended_at FROM alerts ORDER BY opened_at`)
	require.NoError(t, err)
	defer rows.Close()

	alerts := make([]alertRow, 0)
	for rows.Next() {
		var alert alertRow
		require.NoError(t, rows.Scan(&alert.status, &alert.openedAt, &alert.endedAt))
		alerts = append(alerts, alert)
	}
	require.NoError(t, rows.Err())

	return alerts
}
