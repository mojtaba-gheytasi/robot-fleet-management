package application

import (
	"time"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

// RobotState is a robot as seen at one moment. Connectivity is part of it
// because it depends on when the robot is read, so the query computes it with
// its clock and the HTTP adapter never needs one.
type RobotState struct {
	Robot        *domain.Robot
	Connectivity domain.Connectivity
}

func robotStateAt(robot *domain.Robot, now time.Time) RobotState {
	return RobotState{Robot: robot, Connectivity: robot.ConnectivityAt(now)}
}
