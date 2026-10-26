package application

import (
	"context"
	"fmt"
	"time"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

type ObservationRecord struct {
	ObservationID  string
	ObservedAt     time.Time
	ReceivedAt     time.Time
	BatteryPercent float64
	PositionX      float64
	PositionY      float64
	OperatingMode  string
	Outcome        string
}

type GetObservationHistoryQuery struct {
	RobotID string
	Limit   int
}

type GetObservationHistory struct {
	fleet FleetReadRepository
}

func NewGetObservationHistory(fleet FleetReadRepository) *GetObservationHistory {
	return &GetObservationHistory{fleet: fleet}
}

func (query *GetObservationHistory) Execute(
	ctx context.Context,
	request GetObservationHistoryQuery,
) ([]ObservationRecord, error) {
	records, err := query.fleet.ObservationHistory(ctx, domain.RobotID(request.RobotID), boundedLimit(request.Limit))
	if err != nil {
		return nil, fmt.Errorf("observation history: %w", err)
	}

	return records, nil
}
