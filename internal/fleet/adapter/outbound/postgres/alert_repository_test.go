//go:build integration

package postgres_test

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/adapter/outbound/postgres"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/application"
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

func TestAlertRepositoryListsEveryUnresolvedAlertWithoutALimit(t *testing.T) {
	db := startPostgres(t)
	// 201 robots, each with one unresolved alert; every third is acknowledged, and ten
	// share each opening time, so the order must break ties by id.
	_, err := db.Exec(`
		INSERT INTO robots (id, site_id, lifecycle, created_at, activated_at)
		SELECT 'robot-' || n, 'site-north', 'ACTIVE', $1, $1 FROM generate_series(1, 201) n`, registeredAt)
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO alerts (robot_id, kind, status, opened_at, acknowledged_at)
		SELECT 'robot-' || n, 'LOW_BATTERY',
			CASE WHEN n % 3 = 0 THEN 'ACKNOWLEDGED' ELSE 'OPEN' END,
			$1::timestamptz + (n / 10) * interval '1 minute',
			CASE WHEN n % 3 = 0 THEN $1::timestamptz + interval '1 day' END
		FROM generate_series(1, 201) n`, checkedAt)
	require.NoError(t, err)

	alerts, err := postgres.NewAlertRepository(db).List(context.Background(), false, 0)
	require.NoError(t, err)

	require.Len(t, alerts, 201)
	listed := make(map[domain.RobotID]bool)
	for i, alert := range alerts {
		listed[alert.RobotID()] = true
		if i > 0 {
			assert.False(t, alert.OpenedAt().After(alerts[i-1].OpenedAt()), "not newest first")
		}
	}
	for n := 1; n <= 201; n++ {
		assert.True(t, listed[domain.RobotID(fmt.Sprintf("robot-%d", n))], "the alert of robot-%d is missing", n)
	}
}

func TestAlertRepositoryAppliesTheLimit(t *testing.T) {
	db := startPostgres(t)
	seedAlerts(t, db)

	alerts, err := postgres.NewAlertRepository(db).List(context.Background(), true, 1)
	require.NoError(t, err)

	assert.Len(t, alerts, 1)
}

func TestAcknowledgeStoresTheAcknowledgement(t *testing.T) {
	db := startPostgres(t)
	alertID := openAlert(t, db, "robot-101")
	acknowledgeAt := checkedAt.Add(time.Minute)

	_, err := application.NewAcknowledgeAlert(postgres.NewAlertRepository(db), clockAt(acknowledgeAt)).
		Execute(context.Background(), string(alertID))
	require.NoError(t, err)

	status, acknowledgedAt := alertState(t, db, alertID)
	assert.Equal(t, "ACKNOWLEDGED", status)
	assert.True(t, acknowledgedAt.Time.Equal(acknowledgeAt))
}

func TestAcknowledgeTwiceDoesNotWriteTheRowAgain(t *testing.T) {
	db := startPostgres(t)
	alertID := openAlert(t, db, "robot-101")
	alerts := postgres.NewAlertRepository(db)
	first := checkedAt.Add(time.Minute)
	_, err := application.NewAcknowledgeAlert(alerts, clockAt(first)).Execute(context.Background(), string(alertID))
	require.NoError(t, err)
	before := alertRowVersion(t, db, alertID)

	_, err = application.NewAcknowledgeAlert(alerts, clockAt(first.Add(time.Hour))).Execute(context.Background(), string(alertID))
	require.NoError(t, err)

	assert.Equal(t, before, alertRowVersion(t, db, alertID))
	_, acknowledgedAt := alertState(t, db, alertID)
	assert.True(t, acknowledgedAt.Time.Equal(first))
}

func TestAcknowledgeAResolvedAlertIsRejected(t *testing.T) {
	db := startPostgres(t)
	alertID := openAlert(t, db, "robot-101")
	_, err := application.NewRecordTelemetry(postgres.NewRobotRepository(db), clockAt(checkedAt.Add(time.Minute))).
		Execute(context.Background(), reading("robot-101", "obs-recovered", 2, 30))
	require.NoError(t, err)

	_, err = application.NewAcknowledgeAlert(postgres.NewAlertRepository(db), clockAt(checkedAt.Add(2*time.Minute))).
		Execute(context.Background(), string(alertID))

	require.ErrorIs(t, err, domain.ErrAlertTerminal)
	status, acknowledgedAt := alertState(t, db, alertID)
	assert.Equal(t, "RESOLVED", status)
	assert.False(t, acknowledgedAt.Valid)
}

func TestAcknowledgeAnAlertThatDoesNotExist(t *testing.T) {
	alerts := postgres.NewAlertRepository(startPostgres(t))

	for _, id := range []string{"7d2f5c1e-0000-4000-8000-000000000404", "not-a-uuid"} {
		t.Run(id, func(t *testing.T) {
			_, err := application.NewAcknowledgeAlert(alerts, clockAt(checkedAt)).Execute(context.Background(), id)

			require.ErrorIs(t, err, application.ErrAlertNotFound)
		})
	}
}

// Many alerts make every interleaving likely; alert_lifecycle_test.go forces each order once.
func TestAcknowledgeRacingAResolutionLosesNoUpdate(t *testing.T) {
	ctx := context.Background()
	db := startPostgres(t)
	alerts := postgres.NewAlertRepository(db)
	robots := postgres.NewRobotRepository(db)
	clock := increasingClock()

	for round := range 5 {
		alertIDs := make(map[domain.RobotID]domain.AlertID)
		for i := range 20 {
			robotID := domain.RobotID(fmt.Sprintf("robot-%d-%d", round, i))
			alertIDs[robotID] = openAlert(t, db, robotID)
		}

		acknowledged := make(map[domain.AlertID]bool)
		var mutex sync.Mutex
		var wait sync.WaitGroup
		for robotID, alertID := range alertIDs {
			wait.Add(2)
			go func() {
				defer wait.Done()
				_, err := application.NewAcknowledgeAlert(alerts, clock).Execute(ctx, string(alertID))
				if err == nil {
					mutex.Lock()
					acknowledged[alertID] = true
					mutex.Unlock()
					return
				}
				assert.ErrorIs(t, err, domain.ErrAlertTerminal)
			}()
			go func() {
				defer wait.Done()
				_, err := application.NewRecordTelemetry(robots, clock).Execute(ctx, reading(robotID, "obs-recovered", 2, 30))
				assert.NoError(t, err)
			}()
		}
		wait.Wait()

		for _, alertID := range alertIDs {
			alert := storedAlert(t, db, alertID)
			assert.Equal(t, "RESOLVED", alert.status, "a resolution was overwritten")
			assert.Equal(t, acknowledged[alertID], alert.acknowledgedAt.Valid, "acknowledged_at does not match the outcome")
			if alert.acknowledgedAt.Valid {
				assert.False(t, alert.endedAt.Time.Before(alert.acknowledgedAt.Time), "ended before it was acknowledged")
			}
		}
	}
}

// openAlert registers the robot and sends a low reading received at checkedAt.
func openAlert(t *testing.T, db *sql.DB, robotID domain.RobotID) domain.AlertID {
	t.Helper()

	robots := postgres.NewRobotRepository(db)
	require.NoError(t, robots.Create(context.Background(), newRobot(t, robotID, "site-north")))
	_, err := application.NewRecordTelemetry(robots, clockAt(checkedAt)).
		Execute(context.Background(), reading(robotID, "obs-low", 1, 18))
	require.NoError(t, err)

	var id string
	require.NoError(t, db.QueryRow(`SELECT id::text FROM alerts WHERE robot_id = $1`, string(robotID)).Scan(&id))

	return domain.AlertID(id)
}

func alertState(t *testing.T, db *sql.DB, id domain.AlertID) (string, sql.NullTime) {
	t.Helper()

	var status string
	var acknowledgedAt sql.NullTime
	require.NoError(t, db.QueryRow(`SELECT status, acknowledged_at FROM alerts WHERE id = $1`, string(id)).
		Scan(&status, &acknowledgedAt))

	return status, acknowledgedAt
}

func alertRowVersion(t *testing.T, db *sql.DB, id domain.AlertID) string {
	t.Helper()

	var xmin string
	require.NoError(t, db.QueryRow(`SELECT xmin::text FROM alerts WHERE id = $1`, string(id)).Scan(&xmin))

	return xmin
}

func clockAt(now time.Time) application.Clock {
	return func() time.Time { return now }
}

func seedAlerts(t *testing.T, db *sql.DB) {
	t.Helper()

	robots := postgres.NewRobotRepository(db)
	for _, id := range []domain.RobotID{"robot-101", "robot-102", "robot-103"} {
		require.NoError(t, robots.Create(context.Background(), newRobot(t, id, "site-north")))
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
