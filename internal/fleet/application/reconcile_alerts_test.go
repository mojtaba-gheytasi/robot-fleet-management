package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/application"
)

func TestReconcileAlertsRunsAtTheClockTime(t *testing.T) {
	reconciler := &fakeReconciler{result: application.AlertReconciliation{Opened: 2, Resolved: 1}}

	result, err := application.NewReconcileAlerts(reconciler, clockAt(fixedNow)).Execute(context.Background())
	require.NoError(t, err)

	assert.Equal(t, application.AlertReconciliation{Opened: 2, Resolved: 1}, result)
	assert.Equal(t, fixedNow, reconciler.now)
}

func TestReconcileAlertsReportsAFailure(t *testing.T) {
	failure := errors.New("database down")

	_, err := application.NewReconcileAlerts(&fakeReconciler{err: failure}, clockAt(fixedNow)).Execute(context.Background())

	require.ErrorIs(t, err, failure)
}

type fakeReconciler struct {
	result application.AlertReconciliation
	err    error
	now    time.Time
}

func (reconciler *fakeReconciler) Reconcile(_ context.Context, now time.Time) (application.AlertReconciliation, error) {
	reconciler.now = now

	return reconciler.result, reconciler.err
}
