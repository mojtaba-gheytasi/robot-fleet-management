package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

var now = time.Date(2026, time.October, 25, 14, 0, 0, 0, time.UTC)

func TestNewRobotStartsActive(t *testing.T) {
	robot, err := domain.NewRobot("robot-101", "site-north", now)
	require.NoError(t, err)

	assert.Equal(t, domain.RobotID("robot-101"), robot.ID())
	assert.Equal(t, domain.SiteID("site-north"), robot.SiteID())
	assert.Equal(t, domain.LifecycleActive, robot.Lifecycle())
	assert.Equal(t, now, robot.RegisteredAt())
}

func TestNewRobotHasUnknownHealthAndConnectivity(t *testing.T) {
	robot, err := domain.NewRobot("robot-101", "site-north", now)
	require.NoError(t, err)

	assert.Equal(t, domain.BatteryHealthUnknown, robot.BatteryHealth())
	assert.Equal(t, domain.ConnectivityUnknown, robot.ConnectivityAt(now.Add(time.Hour)))
}

func TestNewRobotAcceptsEveryAllowedIDCharacter(t *testing.T) {
	_, err := domain.NewRobot("Robot_1.a-2", "site-north", now)

	require.NoError(t, err)
}

func TestNewRobotRejectsInvalidRegistrations(t *testing.T) {
	tests := map[string]struct {
		id     domain.RobotID
		siteID domain.SiteID
	}{
		"empty id":             {id: "", siteID: "site-north"},
		"id with a slash":      {id: "robot/101", siteID: "site-north"},
		"id with a space":      {id: "robot 101", siteID: "site-north"},
		"id with a non-ASCII":  {id: "roböt", siteID: "site-north"},
		"id longer than limit": {id: domain.RobotID(strings.Repeat("r", 65)), siteID: "site-north"},
		"empty site id":        {id: "robot-101", siteID: ""},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := domain.NewRobot(tt.id, tt.siteID, now)

			require.ErrorIs(t, err, domain.ErrInvalidRobot)
		})
	}
}

func TestReconstituteRobotKeepsStoredLifecycle(t *testing.T) {
	robot, err := domain.ReconstituteRobot("robot-101", "site-north", domain.LifecycleInactive, now, now, domain.BatteryHealthUnknown, nil)
	require.NoError(t, err)

	assert.Equal(t, domain.LifecycleInactive, robot.Lifecycle())
}

func TestReconstituteRobotRejectsUnknownLifecycle(t *testing.T) {
	_, err := domain.ReconstituteRobot("robot-101", "site-north", "PAUSED", now, now, domain.BatteryHealthUnknown, nil)

	require.ErrorIs(t, err, domain.ErrInvalidRobot)
}

func TestApplyObservationBecomesTheCurrentState(t *testing.T) {
	robot, err := domain.NewRobot("robot-101", "site-north", now)
	require.NoError(t, err)
	observation := newObservation(t, "obs-1", now, 18.5)

	outcome, err := robot.ApplyObservation(observation, now.Add(time.Second))
	require.NoError(t, err)

	assert.Equal(t, domain.ObservationApplied, outcome)
	latest, ok := robot.LatestObservation()
	require.True(t, ok)
	assert.Equal(t, observation, latest.Observation)
	assert.Equal(t, now.Add(time.Second), latest.ReceivedAt)
}

func TestNewRobotHasNoLatestObservation(t *testing.T) {
	robot, err := domain.NewRobot("robot-101", "site-north", now)
	require.NoError(t, err)

	_, ok := robot.LatestObservation()

	assert.False(t, ok)
}

func TestConnectivityFollowsTheReceiptTimeOfTheLatestObservation(t *testing.T) {
	receivedAt := now.Add(time.Minute)
	tests := map[string]struct {
		readAt time.Time
		want   domain.Connectivity
	}{
		"just received":        {receivedAt, domain.ConnectivityOnline},
		"exactly 10 s later":   {receivedAt.Add(10 * time.Second), domain.ConnectivityOnline},
		"just over 10 s later": {receivedAt.Add(10*time.Second + time.Nanosecond), domain.ConnectivityStale},
		"a long time later":    {receivedAt.Add(time.Hour), domain.ConnectivityStale},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			robot, err := domain.NewRobot("robot-101", "site-north", now)
			require.NoError(t, err)
			// The robot clock is far behind: connectivity must not care.
			robot.ApplyObservation(newObservation(t, "obs-1", now.Add(-time.Hour), 50), receivedAt)

			assert.Equal(t, tt.want, robot.ConnectivityAt(tt.readAt))
		})
	}
}

func TestReconstituteRobotKeepsTheLatestObservation(t *testing.T) {
	latest := domain.AppliedObservation{Observation: newObservation(t, "obs-1", now, 50), ReceivedAt: now}

	robot, err := domain.ReconstituteRobot("robot-101", "site-north", domain.LifecycleActive, now, now, domain.BatteryHealthNormal, &latest)
	require.NoError(t, err)

	stored, ok := robot.LatestObservation()
	require.True(t, ok)
	assert.Equal(t, latest, stored)
}

func TestApplyObservationOnlyLetsANewerObservationBecomeCurrent(t *testing.T) {
	current := now.Add(time.Minute)
	tests := map[string]struct {
		observedAt  time.Time
		wantOutcome domain.ObservationOutcome
		wantLatest  domain.ObservationID
	}{
		"newer":     {current.Add(time.Microsecond), domain.ObservationApplied, "obs-new"},
		"same time": {current, domain.ObservationSuperseded, "obs-current"},
		"older":     {current.Add(-time.Hour), domain.ObservationSuperseded, "obs-current"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			robot, err := domain.NewRobot("robot-101", "site-north", now)
			require.NoError(t, err)
			_, err = robot.ApplyObservation(newObservation(t, "obs-current", current, 50), current)
			require.NoError(t, err)

			outcome, err := robot.ApplyObservation(newObservation(t, "obs-new", tt.observedAt, 40), current.Add(time.Second))
			require.NoError(t, err)

			assert.Equal(t, tt.wantOutcome, outcome)
			latest, _ := robot.LatestObservation()
			assert.Equal(t, tt.wantLatest, latest.Observation.ID())
		})
	}
}

func TestApplyObservationRejectsATimeTooFarAheadOfTheServer(t *testing.T) {
	receivedAt := now.Add(time.Minute)

	robot, err := domain.NewRobot("robot-101", "site-north", now)
	require.NoError(t, err)

	outcome, err := robot.ApplyObservation(newObservation(t, "obs-1", receivedAt.Add(5*time.Second), 50), receivedAt)
	require.NoError(t, err)
	assert.Equal(t, domain.ObservationApplied, outcome)

	_, err = robot.ApplyObservation(
		newObservation(t, "obs-2", receivedAt.Add(5*time.Second+time.Microsecond), 50),
		receivedAt,
	)
	require.ErrorIs(t, err, domain.ErrClockSkewExceeded)
	latest, _ := robot.LatestObservation()
	assert.Equal(t, domain.ObservationID("obs-1"), latest.Observation.ID())
}
