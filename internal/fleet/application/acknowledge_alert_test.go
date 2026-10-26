package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/application"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

func TestAcknowledgeAlertAcknowledgesAnOpenAlertAtTheClockTime(t *testing.T) {
	alerts := alertsWith(t, domain.AlertStatusOpen)
	acknowledgeAt := fixedNow.Add(time.Minute)

	alert, err := application.NewAcknowledgeAlert(alerts, clockAt(acknowledgeAt)).Execute(context.Background(), "alert-1")
	require.NoError(t, err)

	assert.Equal(t, domain.AlertStatusAcknowledged, alert.Status())
	acknowledgedAt, ok := alert.AcknowledgedAt()
	require.True(t, ok)
	assert.Equal(t, acknowledgeAt, acknowledgedAt)
}

func TestAcknowledgeAlertTwiceKeepsTheFirstTime(t *testing.T) {
	alerts := alertsWith(t, domain.AlertStatusOpen)
	first := fixedNow.Add(time.Minute)
	_, err := application.NewAcknowledgeAlert(alerts, clockAt(first)).Execute(context.Background(), "alert-1")
	require.NoError(t, err)

	alert, err := application.NewAcknowledgeAlert(alerts, clockAt(first.Add(time.Hour))).Execute(context.Background(), "alert-1")
	require.NoError(t, err)

	acknowledgedAt, _ := alert.AcknowledgedAt()
	assert.Equal(t, first, acknowledgedAt)
}

func TestAcknowledgeAlertThatEndedIsRejected(t *testing.T) {
	alerts := alertsWith(t, domain.AlertStatusResolved)

	_, err := application.NewAcknowledgeAlert(alerts, clockAt(fixedNow)).Execute(context.Background(), "alert-1")

	require.ErrorIs(t, err, domain.ErrAlertTerminal)
}

func TestAcknowledgeAlertThatDoesNotExist(t *testing.T) {
	_, err := application.NewAcknowledgeAlert(&fakeAlerts{}, clockAt(fixedNow)).Execute(context.Background(), "alert-404")

	require.ErrorIs(t, err, application.ErrAlertNotFound)
}

func alertsWith(t *testing.T, status domain.AlertStatus) *fakeAlerts {
	t.Helper()

	var endedAt *time.Time
	if status == domain.AlertStatusResolved {
		ended := fixedNow
		endedAt = &ended
	}

	alert, err := domain.ReconstituteAlert(
		"alert-1", "robot-101", domain.AlertKindLowBattery, status, fixedNow.Add(-time.Hour), nil, endedAt, "",
	)
	require.NoError(t, err)

	return &fakeAlerts{stored: map[domain.AlertID]*domain.Alert{"alert-1": alert}}
}
