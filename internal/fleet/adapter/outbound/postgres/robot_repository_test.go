//go:build integration

package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/adapter/outbound/postgres"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/application"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

var registeredAt = time.Date(2026, time.October, 25, 14, 0, 0, 0, time.UTC)

func TestRobotRepositoryCreateAndFind(t *testing.T) {
	ctx := context.Background()
	repository := postgres.NewRobotRepository(startPostgres(t))

	created := newRobot(t, "robot-101", "site-north")
	require.NoError(t, repository.Create(ctx, created))

	found, err := repository.FindByID(ctx, "robot-101")
	require.NoError(t, err)

	assert.Equal(t, created.ID(), found.ID())
	assert.Equal(t, created.SiteID(), found.SiteID())
	assert.Equal(t, domain.LifecycleActive, found.Lifecycle())
	assert.True(t, created.RegisteredAt().Equal(found.RegisteredAt()))
}

func TestRobotRepositoryCreateLeavesAnExistingRobotUnchanged(t *testing.T) {
	ctx := context.Background()
	db := startPostgres(t)
	repository := postgres.NewRobotRepository(db)

	require.NoError(t, repository.Create(ctx, newRobot(t, "robot-101", "site-north")))
	_, err := db.ExecContext(ctx, `UPDATE robots SET lifecycle = 'INACTIVE' WHERE id = 'robot-101'`)
	require.NoError(t, err)

	err = repository.Create(ctx, newRobot(t, "robot-101", "site-south"))
	require.ErrorIs(t, err, application.ErrRobotAlreadyExists)

	found, err := repository.FindByID(ctx, "robot-101")
	require.NoError(t, err)
	assert.Equal(t, domain.SiteID("site-north"), found.SiteID())
	assert.Equal(t, domain.LifecycleInactive, found.Lifecycle(), "a repeated create does not reactivate")
}

func TestRobotRepositoryCreateRejectsAnUnknownSite(t *testing.T) {
	ctx := context.Background()
	repository := postgres.NewRobotRepository(startPostgres(t))

	err := repository.Create(ctx, newRobot(t, "robot-101", "site-nowhere"))

	require.ErrorIs(t, err, application.ErrSiteNotFound)
}

func TestRobotRepositoryFindUnknownRobot(t *testing.T) {
	repository := postgres.NewRobotRepository(startPostgres(t))

	_, err := repository.FindByID(context.Background(), "robot-404")

	require.ErrorIs(t, err, application.ErrRobotNotFound)
}

func newRobot(t *testing.T, id domain.RobotID, siteID domain.SiteID) *domain.Robot {
	t.Helper()

	robot, err := domain.NewRobot(id, siteID, registeredAt)
	require.NoError(t, err)

	return robot
}

func TestRobotRepositoryListsRobotsOrderedByID(t *testing.T) {
	ctx := context.Background()
	repository := postgres.NewRobotRepository(startPostgres(t))
	for _, robot := range []*domain.Robot{
		newRobot(t, "robot-201", "site-south"),
		newRobot(t, "robot-102", "site-north"),
		newRobot(t, "robot-101", "site-north"),
	} {
		require.NoError(t, repository.Create(ctx, robot))
	}

	all, err := repository.List(ctx, nil)
	require.NoError(t, err)
	assert.Equal(t, []domain.RobotID{"robot-101", "robot-102", "robot-201"}, robotIDs(all))

	north := domain.SiteID("site-north")
	atNorth, err := repository.List(ctx, &north)
	require.NoError(t, err)
	assert.Equal(t, []domain.RobotID{"robot-101", "robot-102"}, robotIDs(atNorth))
}

func TestRobotRepositoryListsNothingForASiteWithoutRobots(t *testing.T) {
	ctx := context.Background()
	repository := postgres.NewRobotRepository(startPostgres(t))
	require.NoError(t, repository.Create(ctx, newRobot(t, "robot-101", "site-north")))

	south := domain.SiteID("site-south")
	robots, err := repository.List(ctx, &south)

	require.NoError(t, err)
	assert.Empty(t, robots)
}

func robotIDs(robots []*domain.Robot) []domain.RobotID {
	ids := make([]domain.RobotID, 0, len(robots))
	for _, robot := range robots {
		ids = append(ids, robot.ID())
	}

	return ids
}

func TestRobotRepositoryHasNoLatestObservationBeforeTheFirstReport(t *testing.T) {
	ctx := context.Background()
	repository := postgres.NewRobotRepository(startPostgres(t))
	require.NoError(t, repository.Create(ctx, newRobot(t, "robot-101", "site-north")))

	robot, err := repository.FindByID(ctx, "robot-101")
	require.NoError(t, err)

	_, ok := robot.LatestObservation()
	assert.False(t, ok)
}

func TestRobotRepositoryLoadsTheNewestAppliedObservation(t *testing.T) {
	ctx := context.Background()
	repository := postgres.NewRobotRepository(startPostgres(t))
	require.NoError(t, repository.Create(ctx, newRobot(t, "robot-101", "site-north")))

	newer := applied(t, "obs-2", registeredAt.Add(2*time.Second), 18.5, registeredAt.Add(time.Minute))
	older := applied(t, "obs-1", registeredAt.Add(time.Second), 60, registeredAt.Add(2*time.Minute))
	// Inserted newest first, so "latest" cannot mean "last inserted".
	addObservation(t, repository, "robot-101", newer, domain.ObservationApplied)
	addObservation(t, repository, "robot-101", older, domain.ObservationApplied)

	robot, err := repository.FindByID(ctx, "robot-101")
	require.NoError(t, err)

	latest, ok := robot.LatestObservation()
	require.True(t, ok)
	assertSameObservation(t, newer, latest)
}

func TestRobotRepositoryListsRobotsWithTheirLatestObservation(t *testing.T) {
	ctx := context.Background()
	repository := postgres.NewRobotRepository(startPostgres(t))
	require.NoError(t, repository.Create(ctx, newRobot(t, "robot-101", "site-north")))
	require.NoError(t, repository.Create(ctx, newRobot(t, "robot-102", "site-north")))
	reported := applied(t, "obs-1", registeredAt.Add(time.Second), 42.25, registeredAt.Add(time.Minute))
	addObservation(t, repository, "robot-101", reported, domain.ObservationApplied)

	robots, err := repository.List(ctx, nil)
	require.NoError(t, err)
	require.Len(t, robots, 2)

	latest, ok := robots[0].LatestObservation()
	require.True(t, ok)
	assertSameObservation(t, reported, latest)

	_, ok = robots[1].LatestObservation()
	assert.False(t, ok)
}

// Tiny tables favour a sequential scan, so it is switched off to test the query shape.
func TestQueriesUseThePartialIndexForTheCurrentObservation(t *testing.T) {
	ctx := context.Background()
	db := startPostgres(t)

	connection, err := db.Conn(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = connection.Close() })
	_, err = connection.ExecContext(ctx, `SET enable_seqscan = off`)
	require.NoError(t, err)

	queries := map[string]struct {
		query string
		arg   any
	}{
		"find by id":  {postgres.FindRobotByIDQuery, "robot-101"},
		"list":        {postgres.ListRobotsQuery, nil},
		"open alerts": {postgres.OpenLowBatteryAlertsQuery, registeredAt},
	}

	for name, tt := range queries {
		t.Run(name, func(t *testing.T) {
			rows, err := connection.QueryContext(ctx, "EXPLAIN "+tt.query, tt.arg)
			require.NoError(t, err)
			defer rows.Close()

			var plan strings.Builder
			for rows.Next() {
				var line string
				require.NoError(t, rows.Scan(&line))
				plan.WriteString(line + "\n")
			}
			require.NoError(t, rows.Err())

			assert.Contains(t, plan.String(), "telemetry_observations_current_idx", plan.String())
			assert.NotContains(t, plan.String(), "Seq Scan on telemetry_observations", plan.String())
		})
	}
}

func TestRobotRepositorySupersededObservationIsStoredButNotCurrent(t *testing.T) {
	ctx := context.Background()
	db := startPostgres(t)
	repository := postgres.NewRobotRepository(db)
	require.NoError(t, repository.Create(ctx, newRobot(t, "robot-101", "site-north")))
	current := applied(t, "obs-2", registeredAt.Add(2*time.Second), 50, registeredAt.Add(time.Minute))
	addObservation(t, repository, "robot-101", current, domain.ObservationApplied)

	late := applied(t, "obs-3", registeredAt.Add(3*time.Second), 10, registeredAt.Add(2*time.Minute))
	addObservation(t, repository, "robot-101", late, domain.ObservationSuperseded)

	robot, err := repository.FindByID(ctx, "robot-101")
	require.NoError(t, err)
	latest, _ := robot.LatestObservation()
	assert.Equal(t, domain.ObservationID("obs-2"), latest.Observation.ID())
	assert.Equal(t, 2, countObservations(t, db))
}

func TestRobotRepositoryFindsAStoredObservation(t *testing.T) {
	ctx := context.Background()
	repository := postgres.NewRobotRepository(startPostgres(t))
	require.NoError(t, repository.Create(ctx, newRobot(t, "robot-101", "site-north")))
	stored := applied(t, "obs-1", registeredAt.Add(time.Second), 18.5, registeredAt.Add(time.Minute))
	addObservation(t, repository, "robot-101", stored, domain.ObservationApplied)

	err := repository.WithLockedRobot(ctx, "robot-101", func(_ *domain.Robot, log application.RobotTx) error {
		found, ok, err := log.FindObservation(ctx, "obs-1")
		require.NoError(t, err)
		require.True(t, ok)
		assert.True(t, found.SameAs(stored.Observation))

		_, ok, err = log.FindObservation(ctx, "obs-404")
		require.NoError(t, err)
		assert.False(t, ok)

		return nil
	})
	require.NoError(t, err)
}

func TestRobotRepositoryRollsBackWhenTheWorkFails(t *testing.T) {
	ctx := context.Background()
	db := startPostgres(t)
	repository := postgres.NewRobotRepository(db)
	require.NoError(t, repository.Create(ctx, newRobot(t, "robot-101", "site-north")))
	failure := errors.New("rule broken")

	err := repository.WithLockedRobot(ctx, "robot-101", func(_ *domain.Robot, log application.RobotTx) error {
		reading := applied(t, "obs-1", registeredAt.Add(time.Second), 50, registeredAt.Add(time.Minute))
		require.NoError(t, log.AddObservation(ctx, reading.Observation, reading.ReceivedAt, domain.ObservationApplied))

		return failure
	})

	require.ErrorIs(t, err, failure)
	assert.Equal(t, 0, countObservations(t, db))
}

func TestRobotRepositoryLockUnknownRobot(t *testing.T) {
	repository := postgres.NewRobotRepository(startPostgres(t))

	err := repository.WithLockedRobot(context.Background(), "robot-404", func(*domain.Robot, application.RobotTx) error {
		t.Fatal("must not run for an unknown robot")
		return nil
	})

	require.ErrorIs(t, err, application.ErrRobotNotFound)
}

func TestConcurrentReportsForOneRobotNeverRegressCurrentState(t *testing.T) {
	ctx := context.Background()
	db := startPostgres(t)
	repository := postgres.NewRobotRepository(db)
	require.NoError(t, repository.Create(ctx, newRobot(t, "robot-101", "site-north")))
	recordTelemetry := application.NewRecordTelemetry(repository, increasingClock())

	const reports = 20
	observedAt := make([]time.Time, reports)
	for i := range observedAt {
		observedAt[i] = registeredAt.Add(time.Duration(i+1) * time.Second)
	}
	rand.Shuffle(reports, func(i, j int) { observedAt[i], observedAt[j] = observedAt[j], observedAt[i] })

	var wait sync.WaitGroup
	for i, at := range observedAt {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := recordTelemetry.Execute(ctx, telemetryCommand(fmt.Sprintf("obs-%d", i), at))
			assert.NoError(t, err)
		}()
	}
	wait.Wait()

	robot, err := repository.FindByID(ctx, "robot-101")
	require.NoError(t, err)
	latest, _ := robot.LatestObservation()
	assert.True(t, latest.Observation.ObservedAt().Equal(registeredAt.Add(reports*time.Second)))
	assert.Equal(t, reports, countObservations(t, db))

	rows, err := db.QueryContext(ctx, `
		SELECT observed_at FROM telemetry_observations
		WHERE outcome = 'APPLIED'
		ORDER BY received_at`)
	require.NoError(t, err)
	defer rows.Close()

	var previous time.Time
	for rows.Next() {
		var current time.Time
		require.NoError(t, rows.Scan(&current))
		assert.True(t, current.After(previous), "an applied observation is older than one applied before it")
		previous = current
	}
	require.NoError(t, rows.Err())
}

func TestConcurrentResendsOfOneObservationApplyItOnce(t *testing.T) {
	ctx := context.Background()
	db := startPostgres(t)
	repository := postgres.NewRobotRepository(db)
	require.NoError(t, repository.Create(ctx, newRobot(t, "robot-101", "site-north")))
	recordTelemetry := application.NewRecordTelemetry(repository, increasingClock())

	var applied, duplicates atomic.Int32
	var wait sync.WaitGroup
	for range 10 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, err := recordTelemetry.Execute(ctx, telemetryCommand("obs-1", registeredAt.Add(time.Second)))
			if assert.NoError(t, err) {
				switch result.Outcome {
				case application.TelemetryOutcomeApplied:
					applied.Add(1)
				case application.TelemetryOutcomeDuplicate:
					duplicates.Add(1)
				}
			}
		}()
	}
	wait.Wait()

	assert.Equal(t, int32(1), applied.Load())
	assert.Equal(t, int32(9), duplicates.Load())
	assert.Equal(t, 1, countObservations(t, db))
}

func TestRobotRepositoryStoresBatteryHealth(t *testing.T) {
	ctx := context.Background()
	repository := postgres.NewRobotRepository(startPostgres(t))
	require.NoError(t, repository.Create(ctx, newRobot(t, "robot-101", "site-north")))

	reportBattery(t, repository, "obs-1", 1, 18)

	robot, err := repository.FindByID(ctx, "robot-101")
	require.NoError(t, err)
	assert.Equal(t, domain.BatteryHealthLow, robot.BatteryHealth())

	robots, err := repository.List(ctx, nil)
	require.NoError(t, err)
	assert.Equal(t, domain.BatteryHealthLow, robots[0].BatteryHealth())
}

func TestRobotRepositoryWritesBatteryHealthOnlyWhenItChanges(t *testing.T) {
	ctx := context.Background()
	db := startPostgres(t)
	repository := postgres.NewRobotRepository(db)
	require.NoError(t, repository.Create(ctx, newRobot(t, "robot-101", "site-north")))
	rowVersion := func() string {
		var xmin string
		require.NoError(t, db.QueryRow(`SELECT xmin::text FROM robots WHERE id = 'robot-101'`).Scan(&xmin))
		return xmin
	}

	reportBattery(t, repository, "obs-1", 1, 60)
	normal := rowVersion()

	reportBattery(t, repository, "obs-2", 2, 70)
	assert.Equal(t, normal, rowVersion(), "health stayed NORMAL, so the row must not be written")

	reportBattery(t, repository, "obs-3", 3, 10)
	assert.NotEqual(t, normal, rowVersion(), "health became LOW, so the row must be written")
}

func TestRobotRepositoryRollsBackTheBatteryHealthWithTheWork(t *testing.T) {
	ctx := context.Background()
	db := startPostgres(t)
	repository := postgres.NewRobotRepository(db)
	require.NoError(t, repository.Create(ctx, newRobot(t, "robot-101", "site-north")))
	failure := errors.New("rule broken")

	err := repository.WithLockedRobot(ctx, "robot-101", func(robot *domain.Robot, log application.RobotTx) error {
		reading := applied(t, "obs-1", registeredAt.Add(time.Second), 10, registeredAt.Add(time.Minute))
		outcome, err := robot.ApplyObservation(reading.Observation, reading.ReceivedAt)
		require.NoError(t, err)
		require.NoError(t, log.AddObservation(ctx, reading.Observation, reading.ReceivedAt, outcome))
		require.Equal(t, domain.BatteryHealthLow, robot.BatteryHealth())

		return failure
	})
	require.ErrorIs(t, err, failure)

	robot, err := repository.FindByID(ctx, "robot-101")
	require.NoError(t, err)
	assert.Equal(t, domain.BatteryHealthUnknown, robot.BatteryHealth())
	assert.Equal(t, 0, countObservations(t, db))
}

func TestDeactivateClosesTheUnresolvedAlertImmediately(t *testing.T) {
	ctx := context.Background()
	db := startPostgres(t)
	repository := postgres.NewRobotRepository(db)
	alertID := openAlert(t, db, "robot-101")
	deactivateAt := checkedAt.Add(time.Minute)

	_, err := application.NewDeactivateRobot(repository, clockAt(deactivateAt)).Execute(ctx, "robot-101")
	require.NoError(t, err)

	var status, reason string
	var endedAt time.Time
	require.NoError(t, db.QueryRow(`SELECT status, closure_reason, ended_at FROM alerts WHERE id = $1`, string(alertID)).
		Scan(&status, &reason, &endedAt))
	assert.Equal(t, "CLOSED", status)
	assert.Equal(t, "ROBOT_DEACTIVATED", reason)
	assert.True(t, endedAt.Equal(deactivateAt))

	robot, err := repository.FindByID(ctx, "robot-101")
	require.NoError(t, err)
	assert.Equal(t, domain.LifecycleInactive, robot.Lifecycle())
	assert.Equal(t, domain.BatteryHealthLow, robot.BatteryHealth())
	_, ok := robot.LatestObservation()
	assert.True(t, ok, "the last reading stays visible")
}

func TestDeactivateRollsBackTogetherWithTheAlertClose(t *testing.T) {
	ctx := context.Background()
	db := startPostgres(t)
	repository := postgres.NewRobotRepository(db)
	alertID := openAlert(t, db, "robot-101")
	failure := errors.New("rule broken")

	err := repository.WithLockedRobot(ctx, "robot-101", func(robot *domain.Robot, tx application.RobotTx) error {
		robot.Deactivate()
		_, err := tx.CloseUnresolvedAlerts(ctx, domain.ClosureReasonRobotDeactivated, checkedAt)
		require.NoError(t, err)

		return failure
	})
	require.ErrorIs(t, err, failure)

	robot, err := repository.FindByID(ctx, "robot-101")
	require.NoError(t, err)
	assert.Equal(t, domain.LifecycleActive, robot.Lifecycle())
	status, _ := alertState(t, db, alertID)
	assert.Equal(t, "OPEN", status)
}

func TestActivateStoresTheActivationTime(t *testing.T) {
	ctx := context.Background()
	repository := postgres.NewRobotRepository(startPostgres(t))
	require.NoError(t, repository.Create(ctx, newRobot(t, "robot-101", "site-north")))
	_, err := application.NewDeactivateRobot(repository, clockAt(registeredAt.Add(time.Minute))).Execute(ctx, "robot-101")
	require.NoError(t, err)
	activateAt := registeredAt.Add(time.Hour)

	_, err = application.NewActivateRobot(repository, clockAt(activateAt)).Execute(ctx, "robot-101")
	require.NoError(t, err)

	robot, err := repository.FindByID(ctx, "robot-101")
	require.NoError(t, err)
	assert.Equal(t, domain.LifecycleActive, robot.Lifecycle())
	assert.True(t, robot.ActivatedAt().Equal(activateAt))
}

func reportBattery(t *testing.T, repository *postgres.RobotRepository, id string, second int, percent float64) {
	t.Helper()

	command := telemetryCommand(id, registeredAt.Add(time.Duration(second)*time.Second))
	command.BatteryPercent = percent

	_, err := application.NewRecordTelemetry(repository, increasingClock()).Execute(context.Background(), command)
	require.NoError(t, err)
}

// Each call is later than the last, so receipt order is exactly lock order.
func increasingClock() application.Clock {
	var calls atomic.Int64

	return func() time.Time {
		return registeredAt.Add(time.Hour + time.Duration(calls.Add(1))*time.Microsecond)
	}
}

func telemetryCommand(id string, observedAt time.Time) application.RecordTelemetryCommand {
	return application.RecordTelemetryCommand{
		RobotID:        "robot-101",
		ObservationID:  id,
		ObservedAt:     observedAt,
		BatteryPercent: 50,
		PositionX:      1,
		PositionY:      2,
		OperatingMode:  "WORKING",
	}
}

func addObservation(
	t *testing.T,
	repository *postgres.RobotRepository,
	robotID domain.RobotID,
	reading domain.AppliedObservation,
	outcome domain.ObservationOutcome,
) {
	t.Helper()

	err := repository.WithLockedRobot(context.Background(), robotID, func(_ *domain.Robot, log application.RobotTx) error {
		return log.AddObservation(context.Background(), reading.Observation, reading.ReceivedAt, outcome)
	})
	require.NoError(t, err)
}

func countObservations(t *testing.T, db *sql.DB) int {
	t.Helper()

	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM telemetry_observations`).Scan(&count))

	return count
}

func applied(
	t *testing.T,
	id domain.ObservationID,
	observedAt time.Time,
	percent float64,
	receivedAt time.Time,
) domain.AppliedObservation {
	t.Helper()

	battery, err := domain.NewBatteryLevel(percent)
	require.NoError(t, err)
	position, err := domain.NewPosition(12.5, -24.8)
	require.NoError(t, err)
	observation, err := domain.NewObservation(id, observedAt, battery, position, domain.OperatingModeCharging)
	require.NoError(t, err)

	return domain.AppliedObservation{Observation: observation, ReceivedAt: receivedAt}
}

func assertSameObservation(t *testing.T, want, got domain.AppliedObservation) {
	t.Helper()

	assert.Equal(t, want.Observation.ID(), got.Observation.ID())
	assert.True(t, want.Observation.ObservedAt().Equal(got.Observation.ObservedAt()))
	assert.True(t, want.ReceivedAt.Equal(got.ReceivedAt))
	assert.Equal(t, want.Observation.Battery(), got.Observation.Battery())
	assert.Equal(t, want.Observation.Position(), got.Observation.Position())
	assert.Equal(t, want.Observation.OperatingMode(), got.Observation.OperatingMode())
}
