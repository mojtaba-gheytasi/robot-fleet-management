package domain

import (
	"fmt"
	"time"
)

type AlertID string

type AlertKind string

const AlertKindLowBattery AlertKind = "LOW_BATTERY"

type AlertStatus string

const (
	AlertStatusOpen         AlertStatus = "OPEN"
	AlertStatusAcknowledged AlertStatus = "ACKNOWLEDGED"
	AlertStatusResolved     AlertStatus = "RESOLVED"
	AlertStatusClosed       AlertStatus = "CLOSED"
)

type ClosureReason string

const ClosureReasonRobotDeactivated ClosureReason = "ROBOT_DEACTIVATED"

type Alert struct {
	id             AlertID
	robotID        RobotID
	kind           AlertKind
	status         AlertStatus
	openedAt       time.Time
	acknowledgedAt *time.Time
	endedAt        *time.Time
	closureReason  ClosureReason
}

func ReconstituteAlert(
	id AlertID,
	robotID RobotID,
	kind AlertKind,
	status AlertStatus,
	openedAt time.Time,
	acknowledgedAt *time.Time,
	endedAt *time.Time,
	closureReason ClosureReason,
) (*Alert, error) {
	alert := &Alert{
		id:             id,
		robotID:        robotID,
		kind:           kind,
		status:         status,
		openedAt:       openedAt,
		acknowledgedAt: acknowledgedAt,
		endedAt:        endedAt,
		closureReason:  closureReason,
	}

	if err := alert.validate(); err != nil {
		return nil, fmt.Errorf("reconstitute alert: %w", err)
	}

	return alert, nil
}

func (alert *Alert) Acknowledge(now time.Time) error {
	switch alert.status {
	case AlertStatusOpen:
		alert.status = AlertStatusAcknowledged
		alert.acknowledgedAt = &now
		return nil
	case AlertStatusAcknowledged:
		return nil
	default:
		return fmt.Errorf("%w: it is %s", ErrAlertTerminal, alert.status)
	}
}

func (alert *Alert) ID() AlertID {
	return alert.id
}

func (alert *Alert) RobotID() RobotID {
	return alert.robotID
}

func (alert *Alert) Kind() AlertKind {
	return alert.kind
}

func (alert *Alert) Status() AlertStatus {
	return alert.status
}

func (alert *Alert) OpenedAt() time.Time {
	return alert.openedAt
}

func (alert *Alert) AcknowledgedAt() (time.Time, bool) {
	return optionalTime(alert.acknowledgedAt)
}

func (alert *Alert) EndedAt() (time.Time, bool) {
	return optionalTime(alert.endedAt)
}

func (alert *Alert) ClosureReason() ClosureReason {
	return alert.closureReason
}

func (alert *Alert) validate() error {
	if alert.id == "" || alert.robotID == "" {
		return fmt.Errorf("%w: id and robot id are required", ErrInvalidAlert)
	}

	if alert.kind != AlertKindLowBattery {
		return fmt.Errorf("%w: unknown kind %q", ErrInvalidAlert, alert.kind)
	}

	switch alert.status {
	case AlertStatusOpen, AlertStatusAcknowledged, AlertStatusResolved, AlertStatusClosed:
	default:
		return fmt.Errorf("%w: unknown status %q", ErrInvalidAlert, alert.status)
	}

	if alert.openedAt.IsZero() {
		return fmt.Errorf("%w: opening time is required", ErrInvalidAlert)
	}

	return nil
}

func optionalTime(value *time.Time) (time.Time, bool) {
	if value == nil {
		return time.Time{}, false
	}

	return *value, true
}
