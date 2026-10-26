package application

import (
	"context"
	"fmt"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

// Online, Stale and LowBattery count active robots only (VIS-03).
type FleetSummary struct {
	Registered       int
	Active           int
	Inactive         int
	Online           int
	Stale            int
	LowBattery       int
	UnresolvedAlerts int
}

type GetFleetSummaryQuery struct {
	SiteID string
}

type GetFleetSummary struct {
	fleet FleetReadRepository
	clock Clock
}

func NewGetFleetSummary(fleet FleetReadRepository, clock Clock) *GetFleetSummary {
	return &GetFleetSummary{fleet: fleet, clock: clock}
}

func (query *GetFleetSummary) Execute(ctx context.Context, request GetFleetSummaryQuery) (FleetSummary, error) {
	var siteID *domain.SiteID
	if request.SiteID != "" {
		filter := domain.SiteID(request.SiteID)
		siteID = &filter
	}

	summary, err := query.fleet.Summary(ctx, siteID, query.clock().Add(-domain.OnlineWindow))
	if err != nil {
		return FleetSummary{}, fmt.Errorf("fleet summary: %w", err)
	}

	return summary, nil
}
