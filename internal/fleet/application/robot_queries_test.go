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

func TestGetRobotReturnsAnUnreportedRobotAsUnknown(t *testing.T) {
	robots := newFakeRobots()
	robots.store(t, "robot-101", "site-north", domain.LifecycleActive, fixedNow)

	state, err := application.NewGetRobot(robots, clockAt(fixedNow)).Execute(context.Background(), "robot-101")
	require.NoError(t, err)

	assert.Equal(t, domain.RobotID("robot-101"), state.Robot.ID())
	assert.Equal(t, domain.ConnectivityUnknown, state.Connectivity)
	assert.Equal(t, domain.BatteryHealthUnknown, state.Robot.BatteryHealth())
}

func TestGetRobotReportsAnUnknownRobot(t *testing.T) {
	_, err := application.NewGetRobot(newFakeRobots(), clockAt(fixedNow)).Execute(context.Background(), "robot-404")

	require.ErrorIs(t, err, application.ErrRobotNotFound)
}

func TestListRobotsWithoutAFilterListsEverySite(t *testing.T) {
	robots := fleetOfThree(t)

	states, err := application.NewListRobots(robots, clockAt(fixedNow)).
		Execute(context.Background(), application.ListRobotsQuery{})
	require.NoError(t, err)

	assert.Nil(t, robots.listedSiteID)
	assert.Equal(t, []domain.RobotID{"robot-101", "robot-102", "robot-201"}, robotIDs(states))
	for _, state := range states {
		assert.Equal(t, domain.ConnectivityUnknown, state.Connectivity)
	}
}

func TestListRobotsFiltersBySite(t *testing.T) {
	robots := fleetOfThree(t)

	states, err := application.NewListRobots(robots, clockAt(fixedNow)).
		Execute(context.Background(), application.ListRobotsQuery{SiteID: "site-north"})
	require.NoError(t, err)

	require.NotNil(t, robots.listedSiteID)
	assert.Equal(t, domain.SiteID("site-north"), *robots.listedSiteID)
	assert.Equal(t, []domain.RobotID{"robot-101", "robot-102"}, robotIDs(states))
}

func fleetOfThree(t *testing.T) *fakeRobots {
	t.Helper()

	robots := newFakeRobots()
	robots.store(t, "robot-101", "site-north", domain.LifecycleActive, fixedNow)
	robots.store(t, "robot-102", "site-north", domain.LifecycleActive, fixedNow)
	robots.store(t, "robot-201", "site-south", domain.LifecycleActive, fixedNow)

	return robots
}

func robotIDs(states []application.RobotState) []domain.RobotID {
	ids := make([]domain.RobotID, 0, len(states))
	for _, state := range states {
		ids = append(ids, state.Robot.ID())
	}

	return ids
}

func clockAt(now time.Time) application.Clock {
	return func() time.Time { return now }
}
