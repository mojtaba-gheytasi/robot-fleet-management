package domain_test

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

func TestBatteryLevelAcceptsUpToTwoDecimals(t *testing.T) {
	tests := map[float64]string{
		0:     "0.00",
		18.5:  "18.50",
		18.55: "18.55",
		19.99: "19.99",
		20:    "20.00",
		100:   "100.00",
	}

	for percent, want := range tests {
		level, err := domain.NewBatteryLevel(percent)
		require.NoError(t, err, percent)

		assert.Equal(t, want, level.String())
		assert.Equal(t, percent, level.Percent())
	}
}

func TestBatteryLevelRejectsInvalidReadings(t *testing.T) {
	for _, percent := range []float64{-0.01, 100.01, 18.505, math.NaN(), math.Inf(1)} {
		_, err := domain.NewBatteryLevel(percent)

		require.ErrorIs(t, err, domain.ErrInvalidObservation, percent)
	}
}

func TestPositionMustBeFinite(t *testing.T) {
	_, err := domain.NewPosition(-12.5, 24.8)
	require.NoError(t, err)

	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		_, err := domain.NewPosition(value, 0)
		require.ErrorIs(t, err, domain.ErrInvalidObservation)

		_, err = domain.NewPosition(0, value)
		require.ErrorIs(t, err, domain.ErrInvalidObservation)
	}
}

func TestOperatingMode(t *testing.T) {
	for _, value := range []string{"IDLE", "WORKING", "CHARGING", "ERROR"} {
		mode, err := domain.NewOperatingMode(value)
		require.NoError(t, err)
		assert.Equal(t, domain.OperatingMode(value), mode)
	}

	for _, value := range []string{"", "working", "SLEEPING"} {
		_, err := domain.NewOperatingMode(value)
		require.ErrorIs(t, err, domain.ErrInvalidObservation, value)
	}
}

func TestNewObservationStoresTheTimeInUTC(t *testing.T) {
	berlin := time.FixedZone("UTC+2", 2*60*60)
	observedAt := time.Date(2026, time.October, 25, 16, 5, 0, 0, berlin)

	observation := newObservation(t, "obs-1", observedAt, 50)

	assert.Equal(t, time.UTC, observation.ObservedAt().Location())
	assert.True(t, observation.ObservedAt().Equal(observedAt))
}

func TestNewObservationRejectsInvalidData(t *testing.T) {
	tests := map[string]struct {
		id         domain.ObservationID
		observedAt time.Time
	}{
		"empty id":             {"", now},
		"id longer than limit": {domain.ObservationID(strings.Repeat("o", 65)), now},
		"missing time":         {"obs-1", time.Time{}},
		"sub-microsecond time": {"obs-1", now.Add(time.Nanosecond)},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := domain.NewObservation(tt.id, tt.observedAt, battery(t, 50), position(t), domain.OperatingModeIdle)

			require.ErrorIs(t, err, domain.ErrInvalidObservation)
		})
	}
}

func newObservation(t *testing.T, id domain.ObservationID, observedAt time.Time, percent float64) domain.Observation {
	t.Helper()

	observation, err := domain.NewObservation(id, observedAt, battery(t, percent), position(t), domain.OperatingModeWorking)
	require.NoError(t, err)

	return observation
}

func battery(t *testing.T, percent float64) domain.BatteryLevel {
	t.Helper()

	level, err := domain.NewBatteryLevel(percent)
	require.NoError(t, err)

	return level
}

func position(t *testing.T) domain.Position {
	t.Helper()

	value, err := domain.NewPosition(12.5, 24.8)
	require.NoError(t, err)

	return value
}

func TestObservationSameAs(t *testing.T) {
	original := newObservation(t, "obs-1", now, 50)
	berlin := time.FixedZone("UTC+2", 2*60*60)
	sameInstant, err := domain.NewObservation("obs-1", now.In(berlin), battery(t, 50), position(t), domain.OperatingModeWorking)
	require.NoError(t, err)
	assert.True(t, original.SameAs(sameInstant))

	otherPosition, err := domain.NewPosition(12.5, 24.9)
	require.NoError(t, err)
	differences := map[string]domain.Observation{
		"id":       newObservation(t, "obs-2", now, 50),
		"time":     newObservation(t, "obs-1", now.Add(time.Microsecond), 50),
		"battery":  newObservation(t, "obs-1", now, 50.01),
		"position": mustObservation(t, "obs-1", now, battery(t, 50), otherPosition, domain.OperatingModeWorking),
		"mode":     mustObservation(t, "obs-1", now, battery(t, 50), position(t), domain.OperatingModeIdle),
	}

	for name, other := range differences {
		t.Run(name, func(t *testing.T) {
			assert.False(t, original.SameAs(other))
		})
	}
}

func mustObservation(
	t *testing.T,
	id domain.ObservationID,
	observedAt time.Time,
	level domain.BatteryLevel,
	at domain.Position,
	mode domain.OperatingMode,
) domain.Observation {
	t.Helper()

	observation, err := domain.NewObservation(id, observedAt, level, at, mode)
	require.NoError(t, err)

	return observation
}
