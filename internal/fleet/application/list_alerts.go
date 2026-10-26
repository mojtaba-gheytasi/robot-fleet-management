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

func (query *ListAlerts) Execute(ctx context.Context, request ListAlertsQuery) ([]*domain.Alert, error) {
	alerts, err := query.alerts.List(ctx, request.Terminal, boundedLimit(request.Limit))
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
