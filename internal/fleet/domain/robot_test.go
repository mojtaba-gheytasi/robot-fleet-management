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
	robot, err := domain.ReconstituteRobot("robot-101", "site-north", domain.LifecycleInactive, now)
	require.NoError(t, err)

	assert.Equal(t, domain.LifecycleInactive, robot.Lifecycle())
}

func TestReconstituteRobotRejectsUnknownLifecycle(t *testing.T) {
	_, err := domain.ReconstituteRobot("robot-101", "site-north", "PAUSED", now)

	require.ErrorIs(t, err, domain.ErrInvalidRobot)
}
