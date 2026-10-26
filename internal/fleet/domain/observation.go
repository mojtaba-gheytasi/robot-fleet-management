package domain

import (
	"fmt"
	"time"
)

const maxObservationIDLength = 64

type ObservationID string

type ObservationOutcome string

const (
	ObservationApplied    ObservationOutcome = "APPLIED"
	ObservationSuperseded ObservationOutcome = "SUPERSEDED"
)

type Observation struct {
	id            ObservationID
	observedAt    time.Time
	battery       BatteryLevel
	position      Position
	operatingMode OperatingMode
}

func NewObservation(
	id ObservationID,
	observedAt time.Time,
	battery BatteryLevel,
	position Position,
	operatingMode OperatingMode,
) (Observation, error) {
	if len(id) == 0 || len(id) > maxObservationIDLength {
		return Observation{}, fmt.Errorf(
			"%w: observation id must be 1 to %d characters",
			ErrInvalidObservation,
			maxObservationIDLength,
		)
	}

	if observedAt.IsZero() {
		return Observation{}, fmt.Errorf("%w: observation time is required", ErrInvalidObservation)
	}

	// PostgreSQL stores microseconds; a finer time would never match on resend.
	if observedAt.Truncate(time.Microsecond).Equal(observedAt) == false {
		return Observation{}, fmt.Errorf(
			"%w: observation time has more than microsecond precision",
			ErrInvalidObservation,
		)
	}

	return Observation{
		id:            id,
		observedAt:    observedAt.UTC(),
		battery:       battery,
		position:      position,
		operatingMode: operatingMode,
	}, nil
}

func (observation Observation) ID() ObservationID {
	return observation.id
}

func (observation Observation) ObservedAt() time.Time {
	return observation.observedAt
}

func (observation Observation) Battery() BatteryLevel {
	return observation.battery
}

func (observation Observation) Position() Position {
	return observation.position
}

func (observation Observation) OperatingMode() OperatingMode {
	return observation.operatingMode
}

func (observation Observation) SameAs(other Observation) bool {
	return observation.id == other.id &&
		observation.observedAt.Equal(other.observedAt) &&
		observation.battery == other.battery &&
		observation.position == other.position &&
		observation.operatingMode == other.operatingMode
}

type AppliedObservation struct {
	Observation Observation
	ReceivedAt  time.Time
}
