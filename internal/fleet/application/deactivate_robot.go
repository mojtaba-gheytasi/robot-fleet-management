package application

import (
	"context"
	"fmt"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

type DeactivateRobot struct {
	robots RobotRepository
	clock  Clock
}

func NewDeactivateRobot(robots RobotRepository, clock Clock) *DeactivateRobot {
	return &DeactivateRobot{robots: robots, clock: clock}
}

// Execute changes two aggregates in one transaction on purpose: the alert is
// closed by the deactivation itself (ALT-06), not later.
func (useCase *DeactivateRobot) Execute(ctx context.Context, robotID string) (RobotState, error) {
	var state RobotState

	err := useCase.robots.WithLockedRobot(ctx, domain.RobotID(robotID), func(robot *domain.Robot, tx RobotTx) error {
		robot.Deactivate()

		alert, err := tx.FindUnresolvedAlert(ctx)
		if err != nil {
			return fmt.Errorf("find unresolved alert: %w", err)
		}

		// Read after the alert is locked, like every alert time.
		now := useCase.clock()

		if alert != nil {
			if err := alert.Close(domain.ClosureReasonRobotDeactivated, now); err != nil {
				return err
			}
			if err := tx.SaveAlert(ctx, alert); err != nil {
				return fmt.Errorf("save alert: %w", err)
			}
		}

		state = robotStateAt(robot, now)
		return nil
	})
	if err != nil {
		return RobotState{}, err
	}

	return state, nil
}
