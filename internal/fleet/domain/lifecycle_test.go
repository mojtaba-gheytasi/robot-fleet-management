package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

func TestNewRobotIsActivatedWhenRegistered(t *testing.T) {
	robot, err := domain.NewRobot("robot-101", "site-north", now)
	require.NoError(t, err)

	assert.Equal(t, now, robot.ActivatedAt())
}

func TestDeactivateKeepsTheLastReadingAndHealth(t *testing.T) {
	robot := robotWithHealth(t, domain.BatteryHealthLow)

	robot.Deactivate()
	robot.Deactivate()

	assert.Equal(t, domain.LifecycleInactive, robot.Lifecycle())
	assert.Equal(t, domain.BatteryHealthLow, robot.BatteryHealth())
	latest, ok := robot.LatestObservation()
	require.True(t, ok)
	assert.Equal(t, domain.ObservationID("obs-current"), latest.Observation.ID())
}

func TestActivateAnInactiveRobotRecordsTheTime(t *testing.T) {
	robot := robotWithHealth(t, domain.BatteryHealthLow)
	robot.Deactivate()
	activateAt := now.Add(time.Hour)

	require.NoError(t, robot.Activate(activateAt))

	assert.Equal(t, domain.LifecycleActive, robot.Lifecycle())
	assert.Equal(t, activateAt, robot.ActivatedAt())
	assert.Equal(t, domain.BatteryHealthLow, robot.BatteryHealth(), "reactivation does not change health")
}

func TestActivateAnActiveRobotKeepsItsActivationTime(t *testing.T) {
	robot := robotWithHealth(t, domain.BatteryHealthNormal)

	require.NoError(t, robot.Activate(now.Add(time.Hour)))

	assert.Equal(t, now, robot.ActivatedAt())
}

func TestActivateBeforeRegistrationIsRejected(t *testing.T) {
	robot := robotWithHealth(t, domain.BatteryHealthNormal)
	robot.Deactivate()

	err := robot.Activate(now.Add(-time.Second))

	require.ErrorIs(t, err, domain.ErrInvalidRobot)
	assert.Equal(t, domain.LifecycleInactive, robot.Lifecycle())
	assert.Equal(t, now, robot.ActivatedAt())
}

func TestInactiveRobotRejectsNewObservations(t *testing.T) {
	robot := robotWithHealth(t, domain.BatteryHealthNormal)
	robot.Deactivate()

	_, err := robot.ApplyObservation(newObservation(t, "obs-new", now.Add(time.Minute), 10), now.Add(time.Minute))

	require.ErrorIs(t, err, domain.ErrRobotInactive)
	latest, _ := robot.LatestObservation()
	assert.Equal(t, domain.ObservationID("obs-current"), latest.Observation.ID())
	assert.Equal(t, domain.BatteryHealthNormal, robot.BatteryHealth())
}
