package application

import (
	"context"
	"fmt"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

type GetRobot struct {
	robots RobotRepository
	clock  Clock
}

func NewGetRobot(robots RobotRepository, clock Clock) *GetRobot {
	return &GetRobot{robots: robots, clock: clock}
}

// Execute returns ErrRobotNotFound for any ID that is not stored, including
// one that could never be valid: the caller only needs to know it is not there.
func (query *GetRobot) Execute(ctx context.Context, robotID string) (RobotState, error) {
	robot, err := query.robots.FindByID(ctx, domain.RobotID(robotID))
	if err != nil {
		return RobotState{}, fmt.Errorf("find robot: %w", err)
	}

	return robotStateAt(robot, query.clock()), nil
}
