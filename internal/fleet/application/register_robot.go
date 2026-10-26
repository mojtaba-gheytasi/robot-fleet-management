package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

type RegisterRobotCommand struct {
	RobotID string
	SiteID  string
}

type RegisterRobotResult struct {
	RobotState
	Created bool
}

type RegisterRobot struct {
	robots RobotRepository
	clock  Clock
}

func NewRegisterRobot(robots RobotRepository, clock Clock) *RegisterRobot {
	return &RegisterRobot{robots: robots, clock: clock}
}

func (useCase *RegisterRobot) Execute(
	ctx context.Context,
	command RegisterRobotCommand,
) (RegisterRobotResult, error) {
	robot, err := domain.NewRobot(
		domain.RobotID(command.RobotID),
		domain.SiteID(command.SiteID),
		useCase.clock(),
	)
	if err != nil {
		return RegisterRobotResult{}, err
	}

	err = useCase.robots.Create(ctx, robot)
	if err == nil {
		return RegisterRobotResult{RobotState: robotStateAt(robot, useCase.clock()), Created: true}, nil
	}
	if errors.Is(err, ErrRobotAlreadyExists) == false {
		return RegisterRobotResult{}, fmt.Errorf("create robot: %w", err)
	}

	// REG-01: repeating a registration with the same data succeeds and returns
	// the robot as stored, so a retry after a lost response is safe. It does
	// not reactivate an INACTIVE robot.
	existing, err := useCase.robots.FindByID(ctx, robot.ID())
	if err != nil {
		return RegisterRobotResult{}, fmt.Errorf("load existing robot: %w", err)
	}

	if existing.SiteID() != robot.SiteID() {
		return RegisterRobotResult{}, ErrRobotRegistrationConflict
	}

	return RegisterRobotResult{RobotState: robotStateAt(existing, useCase.clock()), Created: false}, nil
}
