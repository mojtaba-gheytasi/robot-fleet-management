package domain_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

func TestBatteryHealthTransitions(t *testing.T) {
	readings := []float64{0, 19.99, 20, 24.99, 25, 100}
	want := map[domain.BatteryHealth][]domain.BatteryHealth{
		domain.BatteryHealthUnknown: {domain.BatteryHealthLow, domain.BatteryHealthLow, domain.BatteryHealthNormal, domain.BatteryHealthNormal, domain.BatteryHealthNormal, domain.BatteryHealthNormal},
		domain.BatteryHealthNormal:  {domain.BatteryHealthLow, domain.BatteryHealthLow, domain.BatteryHealthNormal, domain.BatteryHealthNormal, domain.BatteryHealthNormal, domain.BatteryHealthNormal},
		domain.BatteryHealthLow:     {domain.BatteryHealthLow, domain.BatteryHealthLow, domain.BatteryHealthLow, domain.BatteryHealthLow, domain.BatteryHealthNormal, domain.BatteryHealthNormal},
	}

	for start, expected := range want {
		for i, percent := range readings {
			t.Run(fmt.Sprintf("%s at %.2f", start, percent), func(t *testing.T) {
				robot := robotWithHealth(t, start)

				_, err := robot.ApplyObservation(newObservation(t, "obs-new", now.Add(time.Hour), percent), now.Add(time.Hour))
				require.NoError(t, err)

				assert.Equal(t, expected[i], robot.BatteryHealth())
			})
		}
	}
}

func TestBatteryHealthFollowsASequenceOfReadings(t *testing.T) {
	robot, err := domain.NewRobot("robot-101", "site-north", now)
	require.NoError(t, err)
	assert.Equal(t, domain.BatteryHealthUnknown, robot.BatteryHealth())

	steps := []struct {
		percent float64
		want    domain.BatteryHealth
	}{
		{60, domain.BatteryHealthNormal},
		{18, domain.BatteryHealthLow},
		{22, domain.BatteryHealthLow},
		{25, domain.BatteryHealthNormal},
	}

	for i, step := range steps {
		at := now.Add(time.Duration(i+1) * time.Second)
		_, err := robot.ApplyObservation(newObservation(t, domain.ObservationID(fmt.Sprintf("obs-%d", i)), at, step.percent), at)
		require.NoError(t, err)

		assert.Equal(t, step.want, robot.BatteryHealth(), "after %.0f%%", step.percent)
	}
}

func TestSupersededReadingDoesNotChangeBatteryHealth(t *testing.T) {
	robot := robotWithHealth(t, domain.BatteryHealthNormal)

	outcome, err := robot.ApplyObservation(newObservation(t, "obs-late", now.Add(-time.Hour), 5), now.Add(time.Hour))
	require.NoError(t, err)

	assert.Equal(t, domain.ObservationSuperseded, outcome)
	assert.Equal(t, domain.BatteryHealthNormal, robot.BatteryHealth())
}

func TestOperatingModeDoesNotAffectBatteryHealth(t *testing.T) {
	for _, mode := range []domain.OperatingMode{domain.OperatingModeCharging, domain.OperatingModeError} {
		t.Run(string(mode), func(t *testing.T) {
			robot := robotWithHealth(t, domain.BatteryHealthNormal)
			observation := mustObservation(t, "obs-new", now.Add(time.Hour), battery(t, 15), position(t), mode)

			_, err := robot.ApplyObservation(observation, now.Add(time.Hour))
			require.NoError(t, err)

			assert.Equal(t, domain.BatteryHealthLow, robot.BatteryHealth())
		})
	}
}

func TestReconstituteRobotRejectsUnknownBatteryHealth(t *testing.T) {
	_, err := domain.ReconstituteRobot("robot-101", "site-north", domain.LifecycleActive, now, now, "EMPTY", nil)

	require.ErrorIs(t, err, domain.ErrInvalidRobot)
}

func robotWithHealth(t *testing.T, health domain.BatteryHealth) *domain.Robot {
	t.Helper()

	var latest *domain.AppliedObservation
	if health != domain.BatteryHealthUnknown {
		latest = &domain.AppliedObservation{Observation: newObservation(t, "obs-current", now, 50), ReceivedAt: now}
	}

	robot, err := domain.ReconstituteRobot("robot-101", "site-north", domain.LifecycleActive, now, now, health, latest)
	require.NoError(t, err)

	return robot
}
