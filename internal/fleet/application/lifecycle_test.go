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

func TestDeactivateRobotClosesItsAlertInTheSameTransaction(t *testing.T) {
	robots := reportingRobot(t)
	require.Len(t, robots.alerts, 1, "the 18.5% reading opened an alert")
	deactivateAt := fixedNow.Add(time.Hour)

	state, err := application.NewDeactivateRobot(robots, clockAt(deactivateAt)).Execute(context.Background(), "robot-101")
	require.NoError(t, err)

	assert.Equal(t, domain.LifecycleInactive, state.Robot.Lifecycle())
	require.Len(t, robots.alerts, 1)
	closed := robots.alerts[0]
	assert.Equal(t, domain.AlertStatusClosed, closed.Status())
	assert.Equal(t, domain.ClosureReasonRobotDeactivated, closed.ClosureReason())
	endedAt, _ := closed.EndedAt()
	assert.Equal(t, deactivateAt, endedAt)
	_, ok := state.Robot.LatestObservation()
	assert.True(t, ok, "the last reading stays visible")
}

func TestDeactivateRobotWithoutAnAlertClosesNothing(t *testing.T) {
	robots := newFakeRobots()
	robots.store(t, "robot-101", "site-north", domain.LifecycleActive, fixedNow)

	_, err := application.NewDeactivateRobot(robots, clockAt(fixedNow.Add(time.Hour))).Execute(context.Background(), "robot-101")
	require.NoError(t, err)

	assert.Empty(t, robots.alerts)
}

func TestActivateRobotRecordsTheActivationTime(t *testing.T) {
	robots := reportingRobot(t)
	_, err := application.NewDeactivateRobot(robots, clockAt(fixedNow.Add(time.Hour))).Execute(context.Background(), "robot-101")
	require.NoError(t, err)
	activateAt := fixedNow.Add(2 * time.Hour)

	state, err := application.NewActivateRobot(robots, clockAt(activateAt)).Execute(context.Background(), "robot-101")
	require.NoError(t, err)

	assert.Equal(t, domain.LifecycleActive, state.Robot.Lifecycle())
	assert.Equal(t, activateAt, state.Robot.ActivatedAt())
}

func TestLifecycleOfAnUnknownRobot(t *testing.T) {
	_, err := application.NewDeactivateRobot(newFakeRobots(), clockAt(fixedNow)).Execute(context.Background(), "robot-404")
	require.ErrorIs(t, err, application.ErrRobotNotFound)

	_, err = application.NewActivateRobot(newFakeRobots(), clockAt(fixedNow)).Execute(context.Background(), "robot-404")
	require.ErrorIs(t, err, application.ErrRobotNotFound)
}

func TestInactiveRobotRejectsNewTelemetryButAcceptsAResend(t *testing.T) {
	robots := reportingRobot(t)
	_, err := application.NewDeactivateRobot(robots, clockAt(fixedNow.Add(2*time.Minute))).Execute(context.Background(), "robot-101")
	require.NoError(t, err)

	resend, err := recordTelemetry(robots, fixedNow.Add(3*time.Minute), validTelemetry())
	require.NoError(t, err)
	assert.Equal(t, application.TelemetryOutcomeDuplicate, resend.Outcome)

	fresh := validTelemetry()
	fresh.ObservationID = "obs-103"
	fresh.ObservedAt = fresh.ObservedAt.Add(time.Minute)
	_, err = recordTelemetry(robots, fixedNow.Add(3*time.Minute), fresh)
	require.ErrorIs(t, err, domain.ErrRobotInactive)
	assert.Len(t, robots.added, 1)
}
