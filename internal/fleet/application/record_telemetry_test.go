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
	assert.Equal(t, receivedAt, added.ReceivedAt, "receipt time comes from the server clock")
	assert.Equal(t, domain.ObservationID("obs-102"), added.Observation.ID())
	assert.Equal(t, "18.50", added.Observation.Battery().String())
	assert.Equal(t, 12.5, added.Observation.Position().X())
	assert.Equal(t, 24.8, added.Observation.Position().Y())
	assert.Equal(t, domain.OperatingModeWorking, added.Observation.OperatingMode())
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
