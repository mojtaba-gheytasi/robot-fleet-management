package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/application"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

func TestRecordTelemetryAppliesAValidObservation(t *testing.T) {
	robots := newFakeRobots()
	robots.store(t, "robot-101", "site-north", domain.LifecycleActive, fixedNow)
	receivedAt := fixedNow.Add(time.Minute)

	result, err := recordTelemetry(robots, receivedAt, validTelemetry())
	require.NoError(t, err)

	assert.Equal(t, application.TelemetryOutcomeApplied, result.Outcome)
	require.Len(t, robots.added, 1)
	added := robots.added[0]
	assert.Equal(t, domain.ObservationApplied, added.outcome)
	assert.Equal(t, receivedAt, added.receivedAt, "receipt time comes from the server clock")
	assert.Equal(t, domain.ObservationID("obs-102"), added.observation.ID())
	assert.Equal(t, "18.50", added.observation.Battery().String())
	assert.Equal(t, 12.5, added.observation.Position().X())
	assert.Equal(t, 24.8, added.observation.Position().Y())
	assert.Equal(t, domain.OperatingModeWorking, added.observation.OperatingMode())
}

func TestRecordTelemetryForAnUnknownRobot(t *testing.T) {
	robots := newFakeRobots()

	_, err := recordTelemetry(robots, fixedNow, validTelemetry())

	require.ErrorIs(t, err, application.ErrRobotNotFound)
	assert.Empty(t, robots.added)
}

func TestRecordTelemetryRejectsInvalidValuesBeforeStoring(t *testing.T) {
	tests := map[string]func(*application.RecordTelemetryCommand){
		"battery above 100":      func(c *application.RecordTelemetryCommand) { c.BatteryPercent = 100.5 },
		"battery three decimals": func(c *application.RecordTelemetryCommand) { c.BatteryPercent = 18.505 },
		"unknown mode":           func(c *application.RecordTelemetryCommand) { c.OperatingMode = "SLEEPING" },
		"missing id":             func(c *application.RecordTelemetryCommand) { c.ObservationID = "" },
	}

	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			robots := newFakeRobots()
			robots.store(t, "robot-101", "site-north", domain.LifecycleActive, fixedNow)
			command := validTelemetry()
			change(&command)

			_, err := recordTelemetry(robots, fixedNow, command)

			require.ErrorIs(t, err, domain.ErrInvalidObservation)
			assert.Empty(t, robots.added)
		})
	}
}

func TestRecordTelemetryResendIsADuplicateAndChangesNothing(t *testing.T) {
	robots := reportingRobot(t)

	result, err := recordTelemetry(robots, fixedNow.Add(2*time.Minute), validTelemetry())
	require.NoError(t, err)

	assert.Equal(t, application.TelemetryOutcomeDuplicate, result.Outcome)
	assert.Len(t, robots.added, 1)
}

func TestRecordTelemetrySameIDWithOtherContentIsAConflict(t *testing.T) {
	robots := reportingRobot(t)
	command := validTelemetry()
	command.BatteryPercent = 17

	_, err := recordTelemetry(robots, fixedNow.Add(2*time.Minute), command)

	require.ErrorIs(t, err, domain.ErrObservationConflict)
	assert.Len(t, robots.added, 1)
}

func TestRecordTelemetryOlderObservationIsSupersededAndStored(t *testing.T) {
	robots := reportingRobot(t)
	older := validTelemetry()
	older.ObservationID = "obs-101"
	older.ObservedAt = older.ObservedAt.Add(-time.Second)
	older.BatteryPercent = 90

	result, err := recordTelemetry(robots, fixedNow.Add(2*time.Minute), older)
	require.NoError(t, err)

	assert.Equal(t, application.TelemetryOutcomeSuperseded, result.Outcome)
	require.Len(t, robots.added, 2)
	assert.Equal(t, domain.ObservationSuperseded, robots.added[1].outcome)
	latest, _ := robots.stored["robot-101"].LatestObservation()
	assert.Equal(t, domain.ObservationID("obs-102"), latest.Observation.ID(), "current state is unchanged")
}

func TestRecordTelemetryRejectsATimeTooFarAhead(t *testing.T) {
	robots := newFakeRobots()
	robots.store(t, "robot-101", "site-north", domain.LifecycleActive, fixedNow)
	command := validTelemetry()
	command.ObservedAt = fixedNow.Add(time.Minute)

	_, err := recordTelemetry(robots, fixedNow, command)

	require.ErrorIs(t, err, domain.ErrClockSkewExceeded)
	assert.Empty(t, robots.added)
}

func TestRobotIsOnlineAfterTelemetryAndStaleTenSecondsLater(t *testing.T) {
	robots := newFakeRobots()
	robots.store(t, "robot-101", "site-north", domain.LifecycleActive, fixedNow)
	receivedAt := fixedNow.Add(time.Minute)
	_, err := recordTelemetry(robots, receivedAt, validTelemetry())
	require.NoError(t, err)

	online, err := application.NewGetRobot(robots, clockAt(receivedAt.Add(10*time.Second))).
		Execute(context.Background(), "robot-101")
	require.NoError(t, err)
	assert.Equal(t, domain.ConnectivityOnline, online.Connectivity)

	stale, err := application.NewGetRobot(robots, clockAt(receivedAt.Add(11*time.Second))).
		Execute(context.Background(), "robot-101")
	require.NoError(t, err)
	assert.Equal(t, domain.ConnectivityStale, stale.Connectivity)
}

func reportingRobot(t *testing.T) *fakeRobots {
	t.Helper()

	robots := newFakeRobots()
	robots.store(t, "robot-101", "site-north", domain.LifecycleActive, fixedNow)
	_, err := recordTelemetry(robots, fixedNow.Add(time.Minute), validTelemetry())
	require.NoError(t, err)

	return robots
}

func recordTelemetry(
	robots *fakeRobots,
	now time.Time,
	command application.RecordTelemetryCommand,
) (application.RecordTelemetryResult, error) {
	return application.NewRecordTelemetry(robots, clockAt(now)).Execute(context.Background(), command)
}

func validTelemetry() application.RecordTelemetryCommand {
	return application.RecordTelemetryCommand{
		RobotID:        "robot-101",
		ObservationID:  "obs-102",
		ObservedAt:     fixedNow.Add(55 * time.Second),
		BatteryPercent: 18.5,
		PositionX:      12.5,
		PositionY:      24.8,
		OperatingMode:  "WORKING",
	}
}
