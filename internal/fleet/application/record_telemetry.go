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

const TelemetryOutcomeApplied TelemetryOutcome = "applied"

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

	robot, err := useCase.robots.FindByID(ctx, domain.RobotID(command.RobotID))
	if err != nil {
		return RecordTelemetryResult{}, fmt.Errorf("find robot: %w", err)
	}

	applied := robot.ApplyObservation(observation, useCase.clock())

	if err := useCase.robots.AddAppliedObservation(ctx, robot.ID(), applied); err != nil {
		return RecordTelemetryResult{}, fmt.Errorf("add applied observation: %w", err)
	}

	return RecordTelemetryResult{Outcome: TelemetryOutcomeApplied}, nil
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
