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

var fixedNow = time.Date(2026, time.October, 25, 14, 0, 0, 0, time.UTC)

func TestRegisterRobotCreatesAnActiveRobot(t *testing.T) {
	robots := newFakeRobots()

	result, err := registerRobot(robots, "robot-101", "site-north")
	require.NoError(t, err)

	assert.True(t, result.Created)
	assert.Equal(t, domain.RobotID("robot-101"), result.Robot.ID())
	assert.Equal(t, domain.SiteID("site-north"), result.Robot.SiteID())
	assert.Equal(t, domain.LifecycleActive, result.Robot.Lifecycle())
	assert.Equal(t, fixedNow, result.Robot.RegisteredAt())
	assert.Contains(t, robots.stored, domain.RobotID("robot-101"))
}

func TestRegisterRobotIdenticalRetryReturnsTheStoredRobotUnchanged(t *testing.T) {
	robots := newFakeRobots()
	registeredEarlier := fixedNow.Add(-time.Hour)
	robots.store(t, "robot-101", "site-north", domain.LifecycleInactive, registeredEarlier)

	result, err := registerRobot(robots, "robot-101", "site-north")
	require.NoError(t, err)

	assert.False(t, result.Created)
	assert.Equal(t, domain.LifecycleInactive, result.Robot.Lifecycle(), "a retry does not reactivate")
	assert.Equal(t, registeredEarlier, result.Robot.RegisteredAt())
}

func TestRegisterRobotWithADifferentSiteIsAConflict(t *testing.T) {
	robots := newFakeRobots()
	robots.store(t, "robot-101", "site-north", domain.LifecycleActive, fixedNow)

	_, err := registerRobot(robots, "robot-101", "site-south")

	require.ErrorIs(t, err, application.ErrRobotRegistrationConflict)
}

func TestRegisterRobotAtAnUnknownSite(t *testing.T) {
	robots := newFakeRobots()
	robots.createErr = application.ErrSiteNotFound

	_, err := registerRobot(robots, "robot-101", "site-nowhere")

	require.ErrorIs(t, err, application.ErrSiteNotFound)
}

func TestRegisterRobotRejectsInvalidDataBeforeStoring(t *testing.T) {
	robots := newFakeRobots()

	_, err := registerRobot(robots, "robot/101", "site-north")

	require.ErrorIs(t, err, domain.ErrInvalidRobot)
	assert.Equal(t, 0, robots.createCalls)
}

func registerRobot(robots *fakeRobots, robotID, siteID string) (application.RegisterRobotResult, error) {
	useCase := application.NewRegisterRobot(robots, func() time.Time { return fixedNow })

	return useCase.Execute(context.Background(), application.RegisterRobotCommand{
		RobotID: robotID,
		SiteID:  siteID,
	})
}
