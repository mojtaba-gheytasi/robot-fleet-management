package application

import (
	"time"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

type RobotState struct {
	Robot        *domain.Robot
	Connectivity domain.Connectivity
}

func robotStateAt(robot *domain.Robot, now time.Time) RobotState {
	return RobotState{Robot: robot, Connectivity: robot.ConnectivityAt(now)}
}
