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
	setBatteryHealth(t, db, "robot-101", domain.BatteryHealthNormal)
	_, err := postgres.NewAlertReconciler(db).Reconcile(context.Background(), checkedAt.Add(time.Minute))
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

func TestAcknowledgeRacingAResolutionLosesNoUpdate(t *testing.T) {
	ctx := context.Background()
	db := startPostgres(t)
	alerts := postgres.NewAlertRepository(db)
	reconciler := postgres.NewAlertReconciler(db)

	for round := range 5 {
		alertIDs := make([]domain.AlertID, 0, 20)
		for i := range 20 {
			id := domain.RobotID(fmt.Sprintf("robot-%d-%d", round, i))
			robotWithBatteryHealth(t, db, id, domain.BatteryHealthLow)
		}
		_, err := reconciler.Reconcile(ctx, checkedAt)
		require.NoError(t, err)
		unresolved, err := alerts.List(ctx, false, 200)
		require.NoError(t, err)
		for _, alert := range unresolved {
			alertIDs = append(alertIDs, alert.ID())
		}
		_, err = db.Exec(`UPDATE robots SET battery_health = 'NORMAL'`)
		require.NoError(t, err)

		acknowledged := make(map[domain.AlertID]bool)
		var mutex sync.Mutex
		var wait sync.WaitGroup
		for _, alertID := range alertIDs {
			wait.Add(1)
			go func() {
				defer wait.Done()
				_, err := application.NewAcknowledgeAlert(alerts, clockAt(checkedAt.Add(time.Second))).Execute(ctx, string(alertID))
				if err == nil {
					mutex.Lock()
					acknowledged[alertID] = true
					mutex.Unlock()
					return
				}
				assert.ErrorIs(t, err, domain.ErrAlertTerminal)
			}()
		}
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := reconciler.Reconcile(ctx, checkedAt.Add(time.Second))
			assert.NoError(t, err)
		}()
		wait.Wait()

		for _, alertID := range alertIDs {
			status, acknowledgedAt := alertState(t, db, alertID)
			assert.Equal(t, "RESOLVED", status, "a resolution was overwritten")
			assert.Equal(t, acknowledged[alertID], acknowledgedAt.Valid, "acknowledged_at does not match the outcome")
		}
	}
}

func openAlert(t *testing.T, db *sql.DB, robotID domain.RobotID) domain.AlertID {
	t.Helper()

	robotWithBatteryHealth(t, db, robotID, domain.BatteryHealthLow)
	_, err := postgres.NewAlertReconciler(db).Reconcile(context.Background(), checkedAt)
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
