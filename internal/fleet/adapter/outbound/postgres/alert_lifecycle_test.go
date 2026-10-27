//go:build integration

package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/adapter/outbound/postgres"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/application"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

var checkedAt = time.Date(2026, time.October, 25, 15, 0, 0, 0, time.UTC)

func TestATransientLowEpisodeIsRecordedAndResolved(t *testing.T) {
	db := startPostgres(t)
	fleet := newFleet(t, db, "robot-101")

	fleet.send(t, "robot-101", 1, 60)
	fleet.send(t, "robot-101", 2, 18)
	fleet.send(t, "robot-101", 3, 30)

	alerts := storedAlerts(t, db)
	require.Len(t, alerts, 1)
	assert.Equal(t, "RESOLVED", alerts[0].status)
	assert.True(t, alerts[0].openedAt.Equal(receivedAtOf(t, db, "obs-2")), "opened when the low reading was received")
	assert.True(t, alerts[0].endedAt.Time.Equal(receivedAtOf(t, db, "obs-3")), "resolved when the recovery was received")
	assert.Equal(t, 0, unresolvedAlerts(t, db))
}

func TestASecondLowEpisodeGetsItsOwnAlert(t *testing.T) {
	ctx := context.Background()
	db := startPostgres(t)
	fleet := newFleet(t, db, "robot-101")
	fleet.send(t, "robot-101", 1, 18)
	first := storedAlerts(t, db)[0]
	_, err := application.NewAcknowledgeAlert(postgres.NewAlertRepository(db), fleet.clock).Execute(ctx, first.id)
	require.NoError(t, err)
	acknowledgedAt := storedAlerts(t, db)[0].acknowledgedAt.Time

	fleet.send(t, "robot-101", 2, 30)
	fleet.send(t, "robot-101", 3, 15)

	alerts := storedAlerts(t, db)
	require.Len(t, alerts, 2)
	assert.Equal(t, first.id, alerts[0].id)
	assert.Equal(t, "RESOLVED", alerts[0].status)
	assert.True(t, alerts[0].acknowledgedAt.Time.Equal(acknowledgedAt), "the first episode keeps its acknowledgement")
	assert.Equal(t, "OPEN", alerts[1].status)
	assert.False(t, alerts[1].acknowledgedAt.Valid, "the second episode is not acknowledged")

	terminal, err := postgres.NewAlertRepository(db).List(ctx, true, 50)
	require.NoError(t, err)
	require.Len(t, terminal, 1)
	unresolved, err := postgres.NewAlertRepository(db).List(ctx, false, 50)
	require.NoError(t, err)
	require.Len(t, unresolved, 1)
	assert.Equal(t, domain.AlertID(alerts[1].id), unresolved[0].ID())
}

func TestSupersededAndResentLowReadingsOpenNoExtraAlert(t *testing.T) {
	db := startPostgres(t)
	fleet := newFleet(t, db, "robot-101")
	fleet.send(t, "robot-101", 2, 60)

	assert.Equal(t, application.TelemetryOutcomeSuperseded, fleet.send(t, "robot-101", 1, 10))
	assert.Empty(t, storedAlerts(t, db), "a late low reading is history only")

	fleet.send(t, "robot-101", 3, 18)
	assert.Equal(t, application.TelemetryOutcomeDuplicate, fleet.send(t, "robot-101", 3, 18))
	fleet.send(t, "robot-101", 4, 15)
	assert.Len(t, storedAlerts(t, db), 1)
}

func TestReactivationWaitsForTheNextAppliedReading(t *testing.T) {
	tests := map[string]struct {
		freshPercent float64
		wantAlerts   int
	}{
		"still low": {18, 2},
		"recovered": {30, 1},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			db := startPostgres(t)
			fleet := newFleet(t, db, "robot-101")
			fleet.send(t, "robot-101", 1, 15)

			_, err := application.NewDeactivateRobot(fleet.robots, fleet.clock).Execute(ctx, "robot-101")
			require.NoError(t, err)
			closed := storedAlerts(t, db)[0]
			assert.Equal(t, "CLOSED", closed.status)
			assert.Equal(t, "ROBOT_DEACTIVATED", closed.closureReason.String)

			_, err = application.NewActivateRobot(fleet.robots, fleet.clock).Execute(ctx, "robot-101")
			require.NoError(t, err)
			assert.Len(t, storedAlerts(t, db), 1, "reactivation alone opens no alert")

			fleet.send(t, "robot-101", 2, tt.freshPercent)

			alerts := storedAlerts(t, db)
			require.Len(t, alerts, tt.wantAlerts)
			assert.Equal(t, closed, alerts[0], "the closed alert is not touched")
			if tt.wantAlerts == 2 {
				assert.Equal(t, "OPEN", alerts[1].status)
			}
		})
	}
}

func TestTheDatabaseRejectsASecondUnresolvedAlert(t *testing.T) {
	ctx := context.Background()
	db := startPostgres(t)
	newFleet(t, db, "robot-101")

	_, err := db.ExecContext(ctx, `
		INSERT INTO alerts (robot_id, kind, status, opened_at) VALUES ('robot-101', 'LOW_BATTERY', 'OPEN', now())`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		INSERT INTO alerts (robot_id, kind, status, opened_at, acknowledged_at)
		VALUES ('robot-101', 'LOW_BATTERY', 'ACKNOWLEDGED', now(), now())`)

	assertViolates(t, err, "23505", "alerts_one_unresolved_idx")
}

func TestTheDatabaseRejectsAnAlertInAnImpossibleState(t *testing.T) {
	tests := map[string]struct {
		status         string
		acknowledgedAt string
		endedAt        string
		closureReason  string
		wantConstraint string
	}{
		"open with an end time":            {"OPEN", "NULL", "$1", "NULL", "alerts_lifecycle_shape"},
		"open with an acknowledgement":     {"OPEN", "$1", "NULL", "NULL", "alerts_lifecycle_shape"},
		"acknowledged with no time":        {"ACKNOWLEDGED", "NULL", "NULL", "NULL", "alerts_lifecycle_shape"},
		"resolved with no end time":        {"RESOLVED", "NULL", "NULL", "NULL", "alerts_lifecycle_shape"},
		"closed with no end time":          {"CLOSED", "NULL", "NULL", "'ROBOT_DEACTIVATED'", "alerts_lifecycle_shape"},
		"acknowledged before it opened":    {"ACKNOWLEDGED", "$1 - interval '2 hours'", "NULL", "NULL", "alerts_chronology"},
		"ended before it opened":           {"RESOLVED", "NULL", "$1 - interval '2 hours'", "NULL", "alerts_chronology"},
		"acknowledged after it was closed": {"CLOSED", "$1 + interval '1 minute'", "$1", "'ROBOT_DEACTIVATED'", "alerts_chronology"},
	}

	db := startPostgres(t)
	newFleet(t, db, "robot-101")

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := db.Exec(fmt.Sprintf(`
				INSERT INTO alerts (robot_id, kind, status, opened_at, acknowledged_at, ended_at, closure_reason)
				VALUES ('robot-101', 'LOW_BATTERY', '%s', $1::timestamptz - interval '1 hour', %s, %s, %s)`,
				tt.status, tt.acknowledgedAt, tt.endedAt, tt.closureReason), checkedAt)

			assertViolates(t, err, "23514", tt.wantConstraint)
		})
	}
}

func TestTheDatabaseRejectsActivationBeforeRegistration(t *testing.T) {
	db := startPostgres(t)
	newFleet(t, db, "robot-101")

	_, err := db.Exec(`UPDATE robots SET activated_at = created_at - interval '1 second' WHERE id = 'robot-101'`)

	assertViolates(t, err, "23514", "robots_activation_after_registration")
}

// The tests below pause one real use case while it holds its locks, start a second one,
// wait until PostgreSQL reports it waiting on a lock, then let the first one finish.

func TestRecoveryWaitingForAnAcknowledgementKeepsIt(t *testing.T) {
	ctx := context.Background()
	db := startPostgres(t)
	alertID := openAlert(t, db, "robot-101")
	gate := newGateClock(checkedAt.Add(time.Minute))

	acknowledged := runAsync(func() error {
		_, err := application.NewAcknowledgeAlert(postgres.NewAlertRepository(db), gate.now).Execute(ctx, string(alertID))
		return err
	})
	<-gate.paused
	recovered := runAsync(func() error {
		_, err := application.NewRecordTelemetry(postgres.NewRobotRepository(db), gate.now).
			Execute(ctx, reading("robot-101", "obs-recovered", 2, 30))
		return err
	})
	waitForALockWait(t, db)
	close(gate.release)

	require.NoError(t, <-acknowledged)
	require.NoError(t, <-recovered)
	alert := storedAlert(t, db, alertID)
	assert.Equal(t, "RESOLVED", alert.status)
	assert.True(t, alert.acknowledgedAt.Time.Equal(gate.at(1)), "the acknowledgement is kept")
	assert.True(t, alert.endedAt.Time.Equal(gate.at(2)), "resolved after the acknowledgement, in lock order")
}

func TestAcknowledgementWaitingForARecoveryIsRejected(t *testing.T) {
	ctx := context.Background()
	db := startPostgres(t)
	alertID := openAlert(t, db, "robot-101")
	gate := newGateClock(checkedAt.Add(time.Minute))

	recovered := runAsync(func() error {
		_, err := application.NewRecordTelemetry(postgres.NewRobotRepository(db), gate.now).
			Execute(ctx, reading("robot-101", "obs-recovered", 2, 30))
		return err
	})
	<-gate.paused
	acknowledged := runAsync(func() error {
		_, err := application.NewAcknowledgeAlert(postgres.NewAlertRepository(db), gate.now).Execute(ctx, string(alertID))
		return err
	})
	waitForALockWait(t, db)
	close(gate.release)

	require.NoError(t, <-recovered)
	require.ErrorIs(t, <-acknowledged, domain.ErrAlertTerminal)
	alert := storedAlert(t, db, alertID)
	assert.Equal(t, "RESOLVED", alert.status)
	assert.False(t, alert.acknowledgedAt.Valid)
	assert.True(t, alert.endedAt.Time.Equal(gate.at(1)))
}

func TestDeactivationWaitingForALowReadingClosesItsAlert(t *testing.T) {
	ctx := context.Background()
	db := startPostgres(t)
	fleet := newFleet(t, db, "robot-101")
	fleet.send(t, "robot-101", 1, 60)
	gate := newGateClock(checkedAt.Add(time.Minute))

	reported := runAsync(func() error {
		_, err := application.NewRecordTelemetry(fleet.robots, gate.now).Execute(ctx, reading("robot-101", "obs-low", 2, 18))
		return err
	})
	<-gate.paused
	deactivated := runAsync(func() error {
		_, err := application.NewDeactivateRobot(fleet.robots, gate.now).Execute(ctx, "robot-101")
		return err
	})
	waitForALockWait(t, db)
	close(gate.release)

	require.NoError(t, <-reported)
	require.NoError(t, <-deactivated)
	alerts := storedAlerts(t, db)
	require.Len(t, alerts, 1)
	assert.Equal(t, "CLOSED", alerts[0].status)
	assert.True(t, alerts[0].openedAt.Equal(gate.at(1)))
	assert.True(t, alerts[0].endedAt.Time.Equal(gate.at(2)))
}

func TestALowReadingWaitingForADeactivationIsRejected(t *testing.T) {
	ctx := context.Background()
	db := startPostgres(t)
	fleet := newFleet(t, db, "robot-101")
	fleet.send(t, "robot-101", 1, 60)
	gate := newGateClock(checkedAt.Add(time.Minute))

	deactivated := runAsync(func() error {
		_, err := application.NewDeactivateRobot(fleet.robots, gate.now).Execute(ctx, "robot-101")
		return err
	})
	<-gate.paused
	reported := runAsync(func() error {
		_, err := application.NewRecordTelemetry(fleet.robots, gate.now).Execute(ctx, reading("robot-101", "obs-low", 2, 18))
		return err
	})
	waitForALockWait(t, db)
	close(gate.release)

	require.NoError(t, <-deactivated)
	require.ErrorIs(t, <-reported, domain.ErrRobotInactive)
	assert.Empty(t, storedAlerts(t, db))
	assert.Equal(t, 1, countObservations(t, db))
}

func TestConcurrentLowReadingsOpenOneAlert(t *testing.T) {
	ctx := context.Background()
	db := startPostgres(t)
	fleet := newFleet(t, db, "robot-101")
	seconds := rand.Perm(20)

	var wait sync.WaitGroup
	for _, second := range seconds {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := application.NewRecordTelemetry(fleet.robots, fleet.clock).
				Execute(ctx, reading("robot-101", fmt.Sprintf("obs-%d", second), second+1, float64(10+second%9)))
			assert.NoError(t, err)
		}()
	}
	wait.Wait()

	alerts := storedAlerts(t, db)
	require.Len(t, alerts, 1)
	assert.Equal(t, "OPEN", alerts[0].status)
}

// Telemetry, acknowledgements, deactivations and activations race on a few robots. Any
// error other than the expected business ones (a deadlock, a constraint) fails the test.
func TestMixedConcurrentOperationsKeepTheAlertRules(t *testing.T) {
	ctx := context.Background()
	db := startPostgres(t)
	robotIDs := []domain.RobotID{"robot-1", "robot-2", "robot-3"}
	fleet := newFleet(t, db, robotIDs...)
	alerts := postgres.NewAlertRepository(db)
	var observations atomic.Int64

	operations := []func(robotID domain.RobotID) error{
		func(robotID domain.RobotID) error {
			n := int(observations.Add(1))
			percent := []float64{10, 22, 30}[n%3]
			_, err := application.NewRecordTelemetry(fleet.robots, fleet.clock).
				Execute(ctx, reading(robotID, fmt.Sprintf("obs-%d", n), n, percent))
			return err
		},
		func(robotID domain.RobotID) error {
			unresolved, err := alerts.List(ctx, false, 200)
			if err != nil {
				return err
			}
			for _, alert := range unresolved {
				if alert.RobotID() == robotID {
					_, err := application.NewAcknowledgeAlert(alerts, fleet.clock).Execute(ctx, string(alert.ID()))
					return err
				}
			}
			return nil
		},
		func(robotID domain.RobotID) error {
			_, err := application.NewDeactivateRobot(fleet.robots, fleet.clock).Execute(ctx, string(robotID))
			return err
		},
		func(robotID domain.RobotID) error {
			_, err := application.NewActivateRobot(fleet.robots, fleet.clock).Execute(ctx, string(robotID))
			return err
		},
	}
	weights := []int{0, 0, 0, 0, 0, 1, 1, 2, 3} // mostly telemetry

	var wait sync.WaitGroup
	for range 300 {
		wait.Add(1)
		operation := operations[weights[rand.IntN(len(weights))]]
		robotID := robotIDs[rand.IntN(len(robotIDs))]
		go func() {
			defer wait.Done()
			err := operation(robotID)
			if err != nil && errors.Is(err, domain.ErrRobotInactive) == false && errors.Is(err, domain.ErrAlertTerminal) == false {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wait.Wait()

	var broken int
	require.NoError(t, db.QueryRow(`
		SELECT count(*) FROM alerts a JOIN robots r ON r.id = a.robot_id
		WHERE a.status IN ('OPEN', 'ACKNOWLEDGED') AND (r.lifecycle = 'INACTIVE' OR r.battery_health <> 'LOW')`).Scan(&broken))
	assert.Equal(t, 0, broken, "an inactive or recovered robot has an unresolved alert")

	var missing int
	require.NoError(t, db.QueryRow(`
		SELECT count(*) FROM robots r
		WHERE r.lifecycle = 'ACTIVE' AND r.battery_health = 'LOW'
			AND (SELECT o.received_at FROM telemetry_observations o
				WHERE o.robot_id = r.id AND o.outcome = 'APPLIED'
				ORDER BY o.observed_at DESC LIMIT 1) > r.activated_at
			AND NOT EXISTS (SELECT 1 FROM alerts a WHERE a.robot_id = r.id AND a.status IN ('OPEN', 'ACKNOWLEDGED'))`).Scan(&missing))
	assert.Equal(t, 0, missing, "a robot low since its last activation has no unresolved alert")

	for _, robotID := range robotIDs {
		_, err := fleet.robots.FindByID(ctx, robotID)
		require.NoError(t, err, "every robot still loads")
	}
	_, err := alerts.List(ctx, true, 200)
	require.NoError(t, err, "every alert still loads")
}

func TestAFailedAlertWriteRollsBackTheWholeReading(t *testing.T) {
	tests := map[string]struct {
		before        float64
		after         float64
		failOn        string
		wantHealth    domain.BatteryHealth
		wantStatuses  []string
		wantRetried   []string
		wantRetryLeft domain.BatteryHealth
	}{
		"opening": {
			before: 60, after: 18, failOn: "INSERT",
			wantHealth: domain.BatteryHealthNormal, wantStatuses: []string{},
			wantRetried: []string{"OPEN"}, wantRetryLeft: domain.BatteryHealthLow,
		},
		"resolving": {
			before: 18, after: 30, failOn: "UPDATE",
			wantHealth: domain.BatteryHealthLow, wantStatuses: []string{"OPEN"},
			wantRetried: []string{"RESOLVED"}, wantRetryLeft: domain.BatteryHealthNormal,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			db := startPostgres(t)
			fleet := newFleet(t, db, "robot-101")
			fleet.send(t, "robot-101", 1, tt.before)
			_, err := db.Exec(`
				CREATE FUNCTION fail_alert_write() RETURNS trigger LANGUAGE plpgsql
				AS $$ BEGIN RAISE EXCEPTION 'alert write failed'; END $$`)
			require.NoError(t, err)
			_, err = db.Exec(`CREATE TRIGGER fail_alert_write BEFORE ` + tt.failOn + ` ON alerts
				FOR EACH ROW EXECUTE FUNCTION fail_alert_write()`)
			require.NoError(t, err)
			command := reading("robot-101", "obs-2", 2, tt.after)

			_, err = application.NewRecordTelemetry(fleet.robots, fleet.clock).Execute(ctx, command)

			require.ErrorContains(t, err, "alert write failed")
			assert.Equal(t, 1, countObservations(t, db), "the observation is rolled back")
			assert.Equal(t, tt.wantHealth, fleet.health(t, "robot-101"), "the health change is rolled back")
			assert.Equal(t, tt.wantStatuses, alertStatuses(t, db), "the alert change is rolled back")

			_, err = db.Exec(`DROP TRIGGER fail_alert_write ON alerts`)
			require.NoError(t, err)
			result, err := application.NewRecordTelemetry(fleet.robots, fleet.clock).Execute(ctx, command)

			require.NoError(t, err)
			assert.Equal(t, application.TelemetryOutcomeApplied, result.Outcome, "the retry is new, not a duplicate")
			assert.Equal(t, 2, countObservations(t, db))
			assert.Equal(t, tt.wantRetryLeft, fleet.health(t, "robot-101"))
			assert.Equal(t, tt.wantRetried, alertStatuses(t, db))
		})
	}
}

func TestAReadingThatRunsOutOfTimeWaitingForALockChangesNothing(t *testing.T) {
	db := startPostgres(t)
	fleet := newFleet(t, db, "robot-101")
	holder, err := db.Begin()
	require.NoError(t, err)
	_, err = holder.Exec(`SELECT id FROM robots WHERE id = 'robot-101' FOR UPDATE`)
	require.NoError(t, err)
	command := reading("robot-101", "obs-low", 1, 18)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err = application.NewRecordTelemetry(fleet.robots, fleet.clock).Execute(ctx, command)

	require.Error(t, err)
	require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
	require.Eventually(t, func() bool { return db.Stats().InUse == 1 }, 5*time.Second, 10*time.Millisecond,
		"only the lock holder's connection is still in use")

	require.NoError(t, holder.Rollback())
	assert.Equal(t, 0, countObservations(t, db))
	assert.Empty(t, storedAlerts(t, db))

	result, err := application.NewRecordTelemetry(fleet.robots, fleet.clock).Execute(context.Background(), command)
	require.NoError(t, err)
	assert.Equal(t, application.TelemetryOutcomeApplied, result.Outcome, "the stopped attempt left nothing behind")
	assert.Equal(t, 1, countObservations(t, db))
	assert.Len(t, storedAlerts(t, db), 1)
}

type fleetFixture struct {
	robots *postgres.RobotRepository
	clock  application.Clock
}

// newFleet registers the robots. Every use case in a test shares its clock, which moves
// forward on each call, so stored times follow the order the use cases ran in.
func newFleet(t *testing.T, db *sql.DB, robotIDs ...domain.RobotID) *fleetFixture {
	t.Helper()

	robots := postgres.NewRobotRepository(db)
	for _, id := range robotIDs {
		require.NoError(t, robots.Create(context.Background(), newRobot(t, id, "site-north")))
	}

	var calls atomic.Int64

	return &fleetFixture{
		robots: robots,
		clock: func() time.Time {
			return checkedAt.Add(time.Duration(calls.Add(1)) * time.Millisecond)
		},
	}
}

// send reports a reading with ID obs-<second>, observed at registeredAt plus second.
func (fleet *fleetFixture) send(t *testing.T, robotID domain.RobotID, second int, percent float64) application.TelemetryOutcome {
	t.Helper()

	result, err := application.NewRecordTelemetry(fleet.robots, fleet.clock).
		Execute(context.Background(), reading(robotID, fmt.Sprintf("obs-%d", second), second, percent))
	require.NoError(t, err)

	return result.Outcome
}

func (fleet *fleetFixture) health(t *testing.T, robotID domain.RobotID) domain.BatteryHealth {
	t.Helper()

	robot, err := fleet.robots.FindByID(context.Background(), robotID)
	require.NoError(t, err)

	return robot.BatteryHealth()
}

func receivedAtOf(t *testing.T, db *sql.DB, observationID string) time.Time {
	t.Helper()

	var receivedAt time.Time
	require.NoError(t, db.QueryRow(`SELECT received_at FROM telemetry_observations WHERE observation_id = $1`, observationID).
		Scan(&receivedAt))

	return receivedAt
}

// gateClock's first call blocks until release is closed, which pauses the calling use
// case while it holds every lock it took. Times are handed out in the order calls return,
// so a use case that reads the clock too early gets the earlier time.
type gateClock struct {
	start   time.Time
	calls   atomic.Int64
	returns atomic.Int64
	paused  chan struct{}
	release chan struct{}
}

func newGateClock(start time.Time) *gateClock {
	return &gateClock{start: start, paused: make(chan struct{}), release: make(chan struct{})}
}

func (gate *gateClock) now() time.Time {
	if gate.calls.Add(1) == 1 {
		close(gate.paused)
		<-gate.release
	}

	return gate.at(int(gate.returns.Add(1)))
}

func (gate *gateClock) at(call int) time.Time {
	return gate.start.Add(time.Duration(call) * time.Millisecond)
}

func runAsync(fn func() error) <-chan error {
	done := make(chan error, 1)
	go func() { done <- fn() }()

	return done
}

func waitForALockWait(t *testing.T, db *sql.DB) {
	t.Helper()

	require.Eventually(t, func() bool {
		var waiting int
		err := db.QueryRow(`
			SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&waiting)
		return err == nil && waiting > 0
	}, 10*time.Second, 5*time.Millisecond, "the second use case never waited for a lock")
}

func assertViolates(t *testing.T, err error, code string, constraint string) {
	t.Helper()

	var postgresError *pgconn.PgError
	require.True(t, errors.As(err, &postgresError), "want a PostgreSQL error, got %v", err)
	assert.Equal(t, code, postgresError.Code)
	assert.Equal(t, constraint, postgresError.ConstraintName)
}

type alertRow struct {
	id             string
	status         string
	openedAt       time.Time
	acknowledgedAt sql.NullTime
	endedAt        sql.NullTime
	closureReason  sql.NullString
}

const selectAlertRows = `SELECT id::text, status, opened_at, acknowledged_at, ended_at, closure_reason FROM alerts`

func storedAlerts(t *testing.T, db *sql.DB) []alertRow {
	t.Helper()

	rows, err := db.Query(selectAlertRows + ` ORDER BY opened_at, id`)
	require.NoError(t, err)
	defer rows.Close()

	alerts := make([]alertRow, 0)
	for rows.Next() {
		var alert alertRow
		require.NoError(t, rows.Scan(&alert.id, &alert.status, &alert.openedAt, &alert.acknowledgedAt, &alert.endedAt, &alert.closureReason))
		alerts = append(alerts, alert)
	}
	require.NoError(t, rows.Err())

	return alerts
}

func storedAlert(t *testing.T, db *sql.DB, id domain.AlertID) alertRow {
	t.Helper()

	var alert alertRow
	require.NoError(t, db.QueryRow(selectAlertRows+` WHERE id = $1`, string(id)).
		Scan(&alert.id, &alert.status, &alert.openedAt, &alert.acknowledgedAt, &alert.endedAt, &alert.closureReason))

	return alert
}

func alertStatuses(t *testing.T, db *sql.DB) []string {
	t.Helper()

	statuses := make([]string, 0)
	for _, alert := range storedAlerts(t, db) {
		statuses = append(statuses, alert.status)
	}

	return statuses
}

func unresolvedAlerts(t *testing.T, db *sql.DB) int {
	t.Helper()

	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM alerts WHERE status IN ('OPEN', 'ACKNOWLEDGED')`).Scan(&count))

	return count
}
