package application

import (
	"context"
	"fmt"
)

type ReconcileAlerts struct {
	alerts AlertReconciler
	clock  Clock
}

func NewReconcileAlerts(alerts AlertReconciler, clock Clock) *ReconcileAlerts {
	return &ReconcileAlerts{alerts: alerts, clock: clock}
}

func (useCase *ReconcileAlerts) Execute(ctx context.Context) (AlertReconciliation, error) {
	result, err := useCase.alerts.Reconcile(ctx, useCase.clock())
	if err != nil {
		return AlertReconciliation{}, fmt.Errorf("reconcile alerts: %w", err)
	}

	return result, nil
}
