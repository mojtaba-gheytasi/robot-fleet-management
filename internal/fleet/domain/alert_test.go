package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

func TestNewLowBatteryAlertStartsOpen(t *testing.T) {
	alert, err := domain.NewLowBatteryAlert("alert-1", "robot-101", now)
	require.NoError(t, err)

	assert.Equal(t, domain.AlertID("alert-1"), alert.ID())
	assert.Equal(t, domain.RobotID("robot-101"), alert.RobotID())
	assert.Equal(t, domain.AlertKindLowBattery, alert.Kind())
	assert.Equal(t, domain.AlertStatusOpen, alert.Status())
	assert.Equal(t, now, alert.OpenedAt())
	_, acknowledged := alert.AcknowledgedAt()
	assert.False(t, acknowledged)
	_, ended := alert.EndedAt()
	assert.False(t, ended)
	assert.Empty(t, alert.ClosureReason())
}

func TestNewLowBatteryAlertRejectsMissingValues(t *testing.T) {
	tests := map[string]struct {
		id       domain.AlertID
		robotID  domain.RobotID
		openedAt time.Time
	}{
		"no id":           {"", "robot-101", now},
		"no robot id":     {"alert-1", "", now},
		"no opening time": {"alert-1", "robot-101", time.Time{}},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := domain.NewLowBatteryAlert(tt.id, tt.robotID, tt.openedAt)

			require.ErrorIs(t, err, domain.ErrInvalidAlert)
		})
	}
}

func TestReconstituteAlertKeepsItsState(t *testing.T) {
	acknowledgedAt := now.Add(time.Minute)
	endedAt := now.Add(2 * time.Minute)

	alert, err := domain.ReconstituteAlert(
		"alert-1", "robot-101", domain.AlertKindLowBattery, domain.AlertStatusClosed, now,
		&acknowledgedAt, &endedAt, domain.ClosureReasonRobotDeactivated,
	)
	require.NoError(t, err)

	assert.Equal(t, domain.AlertStatusClosed, alert.Status())
	assert.Equal(t, now, alert.OpenedAt())
	acknowledged, ok := alert.AcknowledgedAt()
	require.True(t, ok)
	assert.Equal(t, acknowledgedAt, acknowledged)
	ended, ok := alert.EndedAt()
	require.True(t, ok)
	assert.Equal(t, endedAt, ended)
	assert.Equal(t, domain.ClosureReasonRobotDeactivated, alert.ClosureReason())
}

func TestReconstituteAlertDoesNotShareTheCallersTimes(t *testing.T) {
	acknowledgedAt := now.Add(time.Minute)
	alert, err := domain.ReconstituteAlert(
		"alert-1", "robot-101", domain.AlertKindLowBattery, domain.AlertStatusAcknowledged, now, &acknowledgedAt, nil, "",
	)
	require.NoError(t, err)

	acknowledgedAt = now.Add(time.Hour)

	stored, _ := alert.AcknowledgedAt()
	assert.Equal(t, now.Add(time.Minute), stored)
}

func TestReconstituteAlertRejectsImpossibleState(t *testing.T) {
	before := now.Add(-time.Minute)
	later := now.Add(time.Minute)
	latest := now.Add(2 * time.Minute)

	tests := map[string]struct {
		id             domain.AlertID
		kind           domain.AlertKind
		status         domain.AlertStatus
		openedAt       time.Time
		acknowledgedAt *time.Time
		endedAt        *time.Time
		closureReason  domain.ClosureReason
	}{
		"empty id":        {id: "", kind: domain.AlertKindLowBattery, status: domain.AlertStatusOpen, openedAt: now},
		"unknown kind":    {id: "alert-1", kind: "OVERHEAT", status: domain.AlertStatusOpen, openedAt: now},
		"unknown status":  {id: "alert-1", kind: domain.AlertKindLowBattery, status: "SNOOZED", openedAt: now},
		"no opening time": {id: "alert-1", kind: domain.AlertKindLowBattery, status: domain.AlertStatusOpen},

		"open with an acknowledgement": {
			status: domain.AlertStatusOpen, acknowledgedAt: &later,
		},
		"open with an end time": {
			status: domain.AlertStatusOpen, endedAt: &later,
		},
		"open with a closure reason": {
			status: domain.AlertStatusOpen, closureReason: domain.ClosureReasonRobotDeactivated,
		},
		"acknowledged without an acknowledgement": {
			status: domain.AlertStatusAcknowledged,
		},
		"acknowledged with an end time": {
			status: domain.AlertStatusAcknowledged, acknowledgedAt: &later, endedAt: &latest,
		},
		"resolved without an end time": {
			status: domain.AlertStatusResolved,
		},
		"resolved with a closure reason": {
			status: domain.AlertStatusResolved, endedAt: &later, closureReason: domain.ClosureReasonRobotDeactivated,
		},
		"closed without an end time": {
			status: domain.AlertStatusClosed, closureReason: domain.ClosureReasonRobotDeactivated,
		},
		"closed without a closure reason": {
			status: domain.AlertStatusClosed, endedAt: &later,
		},
		"closed with an unknown reason": {
			status: domain.AlertStatusClosed, endedAt: &later, closureReason: "BATTERY_SWAPPED",
		},
		"acknowledged before it opened": {
			status: domain.AlertStatusAcknowledged, acknowledgedAt: &before,
		},
		"ended before it opened": {
			status: domain.AlertStatusResolved, endedAt: &before,
		},
		"acknowledged after it ended": {
			status: domain.AlertStatusResolved, acknowledgedAt: &latest, endedAt: &later,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			id, kind, openedAt := tt.id, tt.kind, tt.openedAt
			if tt.kind == "" {
				id, kind, openedAt = "alert-1", domain.AlertKindLowBattery, now
			}

			_, err := domain.ReconstituteAlert(
				id, "robot-101", kind, tt.status, openedAt, tt.acknowledgedAt, tt.endedAt, tt.closureReason,
			)

			require.ErrorIs(t, err, domain.ErrInvalidAlert)
		})
	}
}

func TestAlertTransitions(t *testing.T) {
	acknowledgedAt := now.Add(time.Minute)
	endedAt := now.Add(2 * time.Minute)
	at := now.Add(3 * time.Minute)

	type transition struct {
		name string
		do   func(*domain.Alert) error
	}
	acknowledge := transition{"acknowledge", func(alert *domain.Alert) error { return alert.Acknowledge(at) }}
	resolve := transition{"resolve", func(alert *domain.Alert) error { return alert.Resolve(at) }}
	closeAlert := transition{"close", func(alert *domain.Alert) error {
		return alert.Close(domain.ClosureReasonRobotDeactivated, at)
	}}

	tests := []struct {
		from               domain.AlertStatus
		transition         transition
		wantErr            error
		wantStatus         domain.AlertStatus
		wantAcknowledgedAt time.Time
		wantEndedAt        time.Time
		wantClosureReason  domain.ClosureReason
	}{
		{from: domain.AlertStatusOpen, transition: acknowledge,
			wantStatus: domain.AlertStatusAcknowledged, wantAcknowledgedAt: at},
		{from: domain.AlertStatusAcknowledged, transition: acknowledge,
			wantStatus: domain.AlertStatusAcknowledged, wantAcknowledgedAt: acknowledgedAt},
		{from: domain.AlertStatusResolved, transition: acknowledge, wantErr: domain.ErrAlertTerminal,
			wantStatus: domain.AlertStatusResolved, wantEndedAt: endedAt},
		{from: domain.AlertStatusClosed, transition: acknowledge, wantErr: domain.ErrAlertTerminal,
			wantStatus: domain.AlertStatusClosed, wantEndedAt: endedAt, wantClosureReason: domain.ClosureReasonRobotDeactivated},

		{from: domain.AlertStatusOpen, transition: resolve,
			wantStatus: domain.AlertStatusResolved, wantEndedAt: at},
		{from: domain.AlertStatusAcknowledged, transition: resolve,
			wantStatus: domain.AlertStatusResolved, wantAcknowledgedAt: acknowledgedAt, wantEndedAt: at},
		{from: domain.AlertStatusResolved, transition: resolve, wantErr: domain.ErrAlertTerminal,
			wantStatus: domain.AlertStatusResolved, wantEndedAt: endedAt},
		{from: domain.AlertStatusClosed, transition: resolve, wantErr: domain.ErrAlertTerminal,
			wantStatus: domain.AlertStatusClosed, wantEndedAt: endedAt, wantClosureReason: domain.ClosureReasonRobotDeactivated},

		{from: domain.AlertStatusOpen, transition: closeAlert,
			wantStatus: domain.AlertStatusClosed, wantEndedAt: at, wantClosureReason: domain.ClosureReasonRobotDeactivated},
		{from: domain.AlertStatusAcknowledged, transition: closeAlert,
			wantStatus: domain.AlertStatusClosed, wantAcknowledgedAt: acknowledgedAt, wantEndedAt: at,
			wantClosureReason: domain.ClosureReasonRobotDeactivated},
		{from: domain.AlertStatusResolved, transition: closeAlert, wantErr: domain.ErrAlertTerminal,
			wantStatus: domain.AlertStatusResolved, wantEndedAt: endedAt},
		{from: domain.AlertStatusClosed, transition: closeAlert, wantErr: domain.ErrAlertTerminal,
			wantStatus: domain.AlertStatusClosed, wantEndedAt: endedAt, wantClosureReason: domain.ClosureReasonRobotDeactivated},
	}

	for _, tt := range tests {
		t.Run(tt.transition.name+" from "+string(tt.from), func(t *testing.T) {
			alert := alertIn(t, tt.from, acknowledgedAt, endedAt)

			err := tt.transition.do(alert)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantStatus, alert.Status())
			assert.Equal(t, now, alert.OpenedAt(), "the opening time never changes")
			gotAcknowledgedAt, _ := alert.AcknowledgedAt()
			assert.Equal(t, tt.wantAcknowledgedAt, gotAcknowledgedAt)
			gotEndedAt, _ := alert.EndedAt()
			assert.Equal(t, tt.wantEndedAt, gotEndedAt)
			assert.Equal(t, tt.wantClosureReason, alert.ClosureReason())
		})
	}
}

func TestAlertTransitionsRejectTimesThatRunBackwards(t *testing.T) {
	acknowledgedAt := now.Add(time.Minute)

	tests := map[string]struct {
		from domain.AlertStatus
		do   func(*domain.Alert) error
	}{
		"acknowledge before it opened": {domain.AlertStatusOpen, func(alert *domain.Alert) error {
			return alert.Acknowledge(now.Add(-time.Second))
		}},
		"resolve before it opened": {domain.AlertStatusOpen, func(alert *domain.Alert) error {
			return alert.Resolve(now.Add(-time.Second))
		}},
		"resolve before it was acknowledged": {domain.AlertStatusAcknowledged, func(alert *domain.Alert) error {
			return alert.Resolve(acknowledgedAt.Add(-time.Second))
		}},
		"close before it was acknowledged": {domain.AlertStatusAcknowledged, func(alert *domain.Alert) error {
			return alert.Close(domain.ClosureReasonRobotDeactivated, acknowledgedAt.Add(-time.Second))
		}},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			alert := alertIn(t, tt.from, acknowledgedAt, time.Time{})

			err := tt.do(alert)

			require.ErrorIs(t, err, domain.ErrInvalidAlert)
			assert.Equal(t, tt.from, alert.Status(), "a rejected transition changes nothing")
			_, ended := alert.EndedAt()
			assert.False(t, ended)
		})
	}
}

func TestCloseAlertRejectsAnUnknownReason(t *testing.T) {
	alert := alertIn(t, domain.AlertStatusOpen, time.Time{}, time.Time{})

	err := alert.Close("BATTERY_SWAPPED", now.Add(time.Minute))

	require.ErrorIs(t, err, domain.ErrInvalidAlert)
	assert.Equal(t, domain.AlertStatusOpen, alert.Status())
}

// alertIn builds an alert opened at now in the given status, with the times that status needs.
func alertIn(t *testing.T, status domain.AlertStatus, acknowledgedAt, endedAt time.Time) *domain.Alert {
	t.Helper()

	var acknowledged, ended *time.Time
	var reason domain.ClosureReason
	switch status {
	case domain.AlertStatusAcknowledged:
		acknowledged = &acknowledgedAt
	case domain.AlertStatusResolved:
		ended = &endedAt
	case domain.AlertStatusClosed:
		ended = &endedAt
		reason = domain.ClosureReasonRobotDeactivated
	}

	alert, err := domain.ReconstituteAlert("alert-1", "robot-101", domain.AlertKindLowBattery, status, now, acknowledged, ended, reason)
	require.NoError(t, err)

	return alert
}
