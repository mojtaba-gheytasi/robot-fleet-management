package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

func TestReconstituteAlertKeepsItsState(t *testing.T) {
	endedAt := now.Add(time.Minute)

	alert, err := domain.ReconstituteAlert(
		"alert-1", "robot-101", domain.AlertKindLowBattery, domain.AlertStatusResolved, now, nil, &endedAt, "",
	)
	require.NoError(t, err)

	assert.Equal(t, domain.AlertStatusResolved, alert.Status())
	assert.Equal(t, now, alert.OpenedAt())
	_, acknowledged := alert.AcknowledgedAt()
	assert.False(t, acknowledged)
	ended, ok := alert.EndedAt()
	assert.True(t, ok)
	assert.Equal(t, endedAt, ended)
}

func TestReconstituteAlertRejectsInvalidState(t *testing.T) {
	tests := map[string]struct {
		id       domain.AlertID
		kind     domain.AlertKind
		status   domain.AlertStatus
		openedAt time.Time
	}{
		"empty id":        {"", domain.AlertKindLowBattery, domain.AlertStatusOpen, now},
		"unknown kind":    {"alert-1", "OVERHEAT", domain.AlertStatusOpen, now},
		"unknown status":  {"alert-1", domain.AlertKindLowBattery, "SNOOZED", now},
		"no opening time": {"alert-1", domain.AlertKindLowBattery, domain.AlertStatusOpen, time.Time{}},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := domain.ReconstituteAlert(tt.id, "robot-101", tt.kind, tt.status, tt.openedAt, nil, nil, "")

			require.ErrorIs(t, err, domain.ErrInvalidAlert)
		})
	}
}
