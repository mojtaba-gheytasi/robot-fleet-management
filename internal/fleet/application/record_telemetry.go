package application

import (
	"context"
	"fmt"
	"time"

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
		func(robot *domain.Robot, log ObservationLog) error {
			stored, found, err := log.Find(ctx, observation.ID())
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

			receivedAt := useCase.clock()
			recorded, err := robot.ApplyObservation(observation, receivedAt)
			if err != nil {
				return err
			}

			if err := log.Add(ctx, observation, receivedAt, recorded); err != nil {
				return fmt.Errorf("add observation: %w", err)
			}

			outcome = TelemetryOutcomeApplied
			if recorded == domain.ObservationSuperseded {
				outcome = TelemetryOutcomeSuperseded
			}

			return nil
		},
	)
	if err != nil {
		return RecordTelemetryResult{}, err
	}

	return RecordTelemetryResult{Outcome: outcome}, nil
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
