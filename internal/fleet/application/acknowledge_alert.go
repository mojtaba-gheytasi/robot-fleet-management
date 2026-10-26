package application

import (
	"context"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

type AcknowledgeAlert struct {
	alerts AlertRepository
	clock  Clock
}

func NewAcknowledgeAlert(alerts AlertRepository, clock Clock) *AcknowledgeAlert {
	return &AcknowledgeAlert{alerts: alerts, clock: clock}
}

func (useCase *AcknowledgeAlert) Execute(ctx context.Context, alertID string) (*domain.Alert, error) {
	var acknowledged *domain.Alert

	err := useCase.alerts.WithLockedAlert(ctx, domain.AlertID(alertID), func(alert *domain.Alert) error {
		if err := alert.Acknowledge(useCase.clock()); err != nil {
			return err
		}

		acknowledged = alert
		return nil
	})
	if err != nil {
		return nil, err
	}

	return acknowledged, nil
}
