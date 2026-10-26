//go:build integration

package postgres_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/adapter/outbound/postgres"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

func TestAlertRepositoryListsUnresolvedAlertsNewestFirst(t *testing.T) {
	db := startPostgres(t)
	seedAlerts(t, db)

	alerts, err := postgres.NewAlertRepository(db).List(context.Background(), false, 50)
	require.NoError(t, err)

	require.Len(t, alerts, 2)
	assert.Equal(t, domain.RobotID("robot-102"), alerts[0].RobotID())
	assert.Equal(t, domain.AlertStatusAcknowledged, alerts[0].Status())
	acknowledgedAt, ok := alerts[0].AcknowledgedAt()
	require.True(t, ok)
	assert.True(t, acknowledgedAt.Equal(checkedAt.Add(3*time.Minute)))
	assert.Equal(t, domain.RobotID("robot-101"), alerts[1].RobotID())
	assert.Equal(t, domain.AlertStatusOpen, alerts[1].Status())
	assert.NotEmpty(t, alerts[1].ID())
}

func TestAlertRepositoryListsTerminalAlertsMostRecentlyEndedFirst(t *testing.T) {
	db := startPostgres(t)
	seedAlerts(t, db)

	alerts, err := postgres.NewAlertRepository(db).List(context.Background(), true, 50)
	require.NoError(t, err)

	require.Len(t, alerts, 2)
	assert.Equal(t, domain.AlertStatusClosed, alerts[0].Status())
	assert.Equal(t, domain.ClosureReasonRobotDeactivated, alerts[0].ClosureReason())
	assert.Equal(t, domain.AlertStatusResolved, alerts[1].Status())
	endedAt, ok := alerts[1].EndedAt()
	require.True(t, ok)
	assert.True(t, endedAt.Equal(checkedAt.Add(time.Minute)))
}

func TestAlertRepositoryAppliesTheLimit(t *testing.T) {
	db := startPostgres(t)
	seedAlerts(t, db)

	alerts, err := postgres.NewAlertRepository(db).List(context.Background(), true, 1)
	require.NoError(t, err)

	assert.Len(t, alerts, 1)
}

func seedAlerts(t *testing.T, db *sql.DB) {
	t.Helper()

	for _, id := range []domain.RobotID{"robot-101", "robot-102", "robot-103"} {
		robotWithBatteryHealth(t, db, id, domain.BatteryHealthLow)
	}

	_, err := db.Exec(`
		INSERT INTO alerts (robot_id, kind, status, opened_at, acknowledged_at, ended_at, closure_reason) VALUES
			('robot-103', 'LOW_BATTERY', 'RESOLVED',     $1::timestamptz,                         NULL, $1::timestamptz + interval '1 minute', NULL),
			('robot-103', 'LOW_BATTERY', 'CLOSED',       $1::timestamptz + interval '2 minutes',  NULL, $1::timestamptz + interval '4 minutes', 'ROBOT_DEACTIVATED'),
			('robot-101', 'LOW_BATTERY', 'OPEN',         $1::timestamptz + interval '1 minute',   NULL, NULL, NULL),
			('robot-102', 'LOW_BATTERY', 'ACKNOWLEDGED', $1::timestamptz + interval '2 minutes',  $1::timestamptz + interval '3 minutes', NULL, NULL)`,
		checkedAt,
	)
	require.NoError(t, err)
}
