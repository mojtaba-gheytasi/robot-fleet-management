package application

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

type RecordTelemetryCommand struct {
	RobotID        string
	ObservationID  string
	ObservedAt     time.Time
	BatteryPercent float64
	PositionX      float64
	PositionY      float64
	OperatingMode  string
}

type TelemetryOutcome string

const (
	TelemetryOutcomeApplied    TelemetryOutcome = "applied"
	TelemetryOutcomeSuperseded TelemetryOutcome = "superseded"
	TelemetryOutcomeDuplicate  TelemetryOutcome = "duplicate"
)

type RecordTelemetryResult struct {
	Outcome TelemetryOutcome
}

type RecordTelemetry struct {
	robots RobotRepository
	clock  Clock
}

func NewRecordTelemetry(robots RobotRepository, clock Clock) *RecordTelemetry {
	return &RecordTelemetry{robots: robots, clock: clock}
}

func (useCase *RecordTelemetry) Execute(
	ctx context.Context,
	command RecordTelemetryCommand,
) (RecordTelemetryResult, error) {
	observation, err := observationFrom(command)
	if err != nil {
		return RecordTelemetryResult{}, err
	}

	var outcome TelemetryOutcome
	err = useCase.robots.WithLockedRobot(
		ctx,
		domain.RobotID(command.RobotID),
		func(robot *domain.Robot, tx RobotTx) error {
			stored, found, err := tx.FindObservation(ctx, observation.ID())
			if err != nil {
				return fmt.Errorf("find observation: %w", err)
			}
			if found {
				if stored.SameAs(observation) == false {
					return domain.ErrObservationConflict
				}

				outcome = TelemetryOutcomeDuplicate
				return nil
			}

			// Locked before the clock is read, so an acknowledgement that held the
			// alert is never stamped later than this observation's resolution.
			alert, err := tx.FindUnresolvedAlert(ctx)
			if err != nil {
				return fmt.Errorf("find unresolved alert: %w", err)
			}

			receivedAt := useCase.clock()
			recorded, err := robot.ApplyObservation(observation, receivedAt)
			if err != nil {
				return err
			}

			if err := tx.AddObservation(ctx, observation, receivedAt, recorded); err != nil {
				return fmt.Errorf("add observation: %w", err)
			}

			if recorded == domain.ObservationSuperseded {
				outcome = TelemetryOutcomeSuperseded
				return nil
			}

			if err := followBatteryHealth(ctx, tx, robot, alert, receivedAt); err != nil {
				return err
			}

			outcome = TelemetryOutcomeApplied
			return nil
		},
	)
	if err != nil {
		return RecordTelemetryResult{}, err
	}

	return RecordTelemetryResult{Outcome: outcome}, nil
}

// followBatteryHealth keeps the rule across Robot and Alert after each applied observation:
// a LOW robot has one unresolved alert, any other robot has none. Checking every applied
// observation, not only a change of health, also opens the alert for the first LOW reading
// after a reactivation (REG-07).
func followBatteryHealth(ctx context.Context, tx RobotTx, robot *domain.Robot, alert *domain.Alert, at time.Time) error {
	low := robot.BatteryHealth() == domain.BatteryHealthLow

	switch {
	case low && alert == nil:
		opened, err := domain.NewLowBatteryAlert(domain.AlertID(uuid.NewString()), robot.ID(), at)
		if err != nil {
			return err
		}
		if err := tx.AddAlert(ctx, opened); err != nil {
			return fmt.Errorf("add alert: %w", err)
		}
	case low == false && alert != nil:
		if err := alert.Resolve(at); err != nil {
			return err
		}
		if err := tx.SaveAlert(ctx, alert); err != nil {
			return fmt.Errorf("save alert: %w", err)
		}
	}

	return nil
}

func observationFrom(command RecordTelemetryCommand) (domain.Observation, error) {
	battery, err := domain.NewBatteryLevel(command.BatteryPercent)
	if err != nil {
		return domain.Observation{}, err
	}

	position, err := domain.NewPosition(command.PositionX, command.PositionY)
	if err != nil {
		return domain.Observation{}, err
	}

	mode, err := domain.NewOperatingMode(command.OperatingMode)
	if err != nil {
		return domain.Observation{}, err
	}

	return domain.NewObservation(
		domain.ObservationID(command.ObservationID),
		command.ObservedAt,
		battery,
		position,
		mode,
	)
}
