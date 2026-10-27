package application

import (
	"context"
	"fmt"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

const (
	defaultListLimit = 50
	maxListLimit     = 200
)

type ListAlertsQuery struct {
	Terminal bool
	Limit    int
}

type ListAlerts struct {
	alerts AlertRepository
}

func NewListAlerts(alerts AlertRepository) *ListAlerts {
	return &ListAlerts{alerts: alerts}
}

// Execute lists every unresolved alert unless a limit is given: a robot has at most one, so
// the list is never longer than the robot list. Terminal alerts are a bounded recent window.
func (query *ListAlerts) Execute(ctx context.Context, request ListAlertsQuery) ([]*domain.Alert, error) {
	limit := request.Limit
	if request.Terminal {
		limit = boundedLimit(limit)
	}

	alerts, err := query.alerts.List(ctx, request.Terminal, limit)
	if err != nil {
		return nil, fmt.Errorf("list alerts: %w", err)
	}

	return alerts, nil
}

func boundedLimit(limit int) int {
	if limit <= 0 {
		return defaultListLimit
	}

	return min(limit, maxListLimit)
}
