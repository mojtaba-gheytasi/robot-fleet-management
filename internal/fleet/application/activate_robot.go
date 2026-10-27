package application

import (
	"context"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

type ActivateRobot struct {
	robots RobotRepository
	clock  Clock
}

func NewActivateRobot(robots RobotRepository, clock Clock) *ActivateRobot {
	return &ActivateRobot{robots: robots, clock: clock}
}

func (useCase *ActivateRobot) Execute(ctx context.Context, robotID string) (RobotState, error) {
	var state RobotState

	err := useCase.robots.WithLockedRobot(ctx, domain.RobotID(robotID), func(robot *domain.Robot, _ RobotTx) error {
		now := useCase.clock()
		if err := robot.Activate(now); err != nil {
			return err
		}

		state = robotStateAt(robot, now)
		return nil
	})
	if err != nil {
		return RobotState{}, err
	}

	return state, nil
}
