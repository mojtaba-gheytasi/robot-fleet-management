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

// A zero acknowledgedAt or endedAt means it has not happened.
type Alert struct {
	id             AlertID
	robotID        RobotID
	kind           AlertKind
	status         AlertStatus
	openedAt       time.Time
	acknowledgedAt time.Time
	endedAt        time.Time
	closureReason  ClosureReason
}

func NewLowBatteryAlert(id AlertID, robotID RobotID, openedAt time.Time) (*Alert, error) {
	alert := &Alert{
		id:       id,
		robotID:  robotID,
		kind:     AlertKindLowBattery,
		status:   AlertStatusOpen,
		openedAt: openedAt,
	}

	if err := alert.validate(); err != nil {
		return nil, err
	}

	return alert, nil
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
		id:            id,
		robotID:       robotID,
		kind:          kind,
		status:        status,
		openedAt:      openedAt,
		closureReason: closureReason,
	}
	if acknowledgedAt != nil {
		alert.acknowledgedAt = *acknowledgedAt
	}
	if endedAt != nil {
		alert.endedAt = *endedAt
	}

	if err := alert.validate(); err != nil {
		return nil, fmt.Errorf("reconstitute alert: %w", err)
	}

	return alert, nil
}

// Acknowledge is idempotent: acknowledging again keeps the first time.
func (alert *Alert) Acknowledge(at time.Time) error {
	switch alert.status {
	case AlertStatusOpen:
		acknowledged := *alert
		acknowledged.status = AlertStatusAcknowledged
		acknowledged.acknowledgedAt = at
		return alert.become(acknowledged)
	case AlertStatusAcknowledged:
		return nil
	default:
		return fmt.Errorf("%w: it is %s", ErrAlertTerminal, alert.status)
	}
}

// Resolve ends the alert because the battery recovered. It keeps any acknowledgement.
func (alert *Alert) Resolve(at time.Time) error {
	return alert.end(AlertStatusResolved, "", at)
}

// Close ends the alert without a recovery. It keeps any acknowledgement.
func (alert *Alert) Close(reason ClosureReason, at time.Time) error {
	return alert.end(AlertStatusClosed, reason, at)
}

func (alert *Alert) end(status AlertStatus, reason ClosureReason, at time.Time) error {
	if alert.isTerminal() {
		return fmt.Errorf("%w: it is %s", ErrAlertTerminal, alert.status)
	}

	ended := *alert
	ended.status = status
	ended.endedAt = at
	ended.closureReason = reason

	return alert.become(ended)
}

// become checks the next state before taking it, so a failed transition changes nothing.
func (alert *Alert) become(next Alert) error {
	if err := next.validate(); err != nil {
		return err
	}

	*alert = next

	return nil
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

func (alert *Alert) isTerminal() bool {
	return alert.status == AlertStatusResolved || alert.status == AlertStatusClosed
}

func (alert *Alert) OpenedAt() time.Time {
	return alert.openedAt
}

func (alert *Alert) AcknowledgedAt() (time.Time, bool) {
	return alert.acknowledgedAt, alert.acknowledgedAt.IsZero() == false
}

func (alert *Alert) EndedAt() (time.Time, bool) {
	return alert.endedAt, alert.endedAt.IsZero() == false
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

	if alert.openedAt.IsZero() {
		return fmt.Errorf("%w: opening time is required", ErrInvalidAlert)
	}

	acknowledged := alert.acknowledgedAt.IsZero() == false
	ended := alert.endedAt.IsZero() == false

	switch alert.status {
	case AlertStatusOpen:
		if acknowledged || ended {
			return fmt.Errorf("%w: an open alert has no acknowledgement or end time", ErrInvalidAlert)
		}
	case AlertStatusAcknowledged:
		if acknowledged == false || ended {
			return fmt.Errorf("%w: an acknowledged alert has an acknowledgement and no end time", ErrInvalidAlert)
		}
	case AlertStatusResolved, AlertStatusClosed:
		if ended == false {
			return fmt.Errorf("%w: a %s alert has an end time", ErrInvalidAlert, alert.status)
		}
	default:
		return fmt.Errorf("%w: unknown status %q", ErrInvalidAlert, alert.status)
	}

	wantReason := ClosureReason("")
	if alert.status == AlertStatusClosed {
		wantReason = ClosureReasonRobotDeactivated
	}
	if alert.closureReason != wantReason {
		return fmt.Errorf("%w: closure reason %q does not fit status %s", ErrInvalidAlert, alert.closureReason, alert.status)
	}

	if acknowledged && alert.acknowledgedAt.Before(alert.openedAt) {
		return fmt.Errorf("%w: acknowledged before it was opened", ErrInvalidAlert)
	}
	if ended && alert.endedAt.Before(alert.openedAt) {
		return fmt.Errorf("%w: ended before it was opened", ErrInvalidAlert)
	}
	if acknowledged && ended && alert.acknowledgedAt.After(alert.endedAt) {
		return fmt.Errorf("%w: acknowledged after it ended", ErrInvalidAlert)
	}

	return nil
}
