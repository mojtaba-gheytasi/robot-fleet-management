package domain

import (
	"fmt"
	"time"
)

const (
	maxRobotIDLength = 64

	OnlineWindow = 10 * time.Second
	maxClockSkew = 5 * time.Second
)

type RobotID string
type SiteID string

type Lifecycle string

const (
	LifecycleActive   Lifecycle = "ACTIVE"
	LifecycleInactive Lifecycle = "INACTIVE"
)

type Connectivity string

const (
	ConnectivityUnknown Connectivity = "UNKNOWN"
	ConnectivityOnline  Connectivity = "ONLINE"
	ConnectivityStale   Connectivity = "STALE"
)

type Robot struct {
	id           RobotID
	siteID       SiteID
	lifecycle    Lifecycle
	registeredAt time.Time
	activatedAt  time.Time

	batteryHealth BatteryHealth
	latest        *AppliedObservation
}

func NewRobot(id RobotID, siteID SiteID, now time.Time) (*Robot, error) {
	robot := &Robot{
		id:            id,
		siteID:        siteID,
		lifecycle:     LifecycleActive,
		registeredAt:  now,
		activatedAt:   now,
		batteryHealth: BatteryHealthUnknown,
	}

	if err := robot.validate(); err != nil {
		return nil, err
	}

	return robot, nil
}

func ReconstituteRobot(
	id RobotID,
	siteID SiteID,
	lifecycle Lifecycle,
	registeredAt time.Time,
	activatedAt time.Time,
	batteryHealth BatteryHealth,
	latest *AppliedObservation,
) (*Robot, error) {
	robot := &Robot{
		id:            id,
		siteID:        siteID,
		lifecycle:     lifecycle,
		registeredAt:  registeredAt,
		activatedAt:   activatedAt,
		batteryHealth: batteryHealth,
	}
	if latest != nil {
		copied := *latest
		robot.latest = &copied
	}

	if err := robot.validate(); err != nil {
		return nil, fmt.Errorf("reconstitute robot: %w", err)
	}

	return robot, nil
}

func (robot *Robot) ID() RobotID {
	return robot.id
}

func (robot *Robot) SiteID() SiteID {
	return robot.siteID
}

func (robot *Robot) Lifecycle() Lifecycle {
	return robot.lifecycle
}

func (robot *Robot) RegisteredAt() time.Time {
	return robot.registeredAt
}

func (robot *Robot) ActivatedAt() time.Time {
	return robot.activatedAt
}

func (robot *Robot) Activate(now time.Time) error {
	if robot.lifecycle == LifecycleActive {
		return nil
	}

	if now.Before(robot.registeredAt) {
		return fmt.Errorf("%w: activation time is before registration", ErrInvalidRobot)
	}

	robot.lifecycle = LifecycleActive
	robot.activatedAt = now

	return nil
}

func (robot *Robot) Deactivate() {
	robot.lifecycle = LifecycleInactive
}

func (robot *Robot) LatestObservation() (AppliedObservation, bool) {
	if robot.latest == nil {
		return AppliedObservation{}, false
	}

	return *robot.latest, true
}

func (robot *Robot) ApplyObservation(observation Observation, receivedAt time.Time) (ObservationOutcome, error) {
	if err := observation.validate(); err != nil {
		return "", err
	}

	if robot.lifecycle == LifecycleInactive {
		return "", ErrRobotInactive
	}

	if observation.ObservedAt().After(receivedAt.Add(maxClockSkew)) {
		return "", fmt.Errorf(
			"%w: observed at %s, more than %s after the server time %s",
			ErrClockSkewExceeded,
			observation.ObservedAt().Format(time.RFC3339Nano),
			maxClockSkew,
			receivedAt.UTC().Format(time.RFC3339Nano),
		)
	}

	if robot.latest != nil && observation.ObservedAt().After(robot.latest.Observation.ObservedAt()) == false {
		return ObservationSuperseded, nil
	}

	robot.latest = &AppliedObservation{Observation: observation, ReceivedAt: receivedAt}
	robot.batteryHealth = robot.batteryHealth.after(observation.Battery())

	return ObservationApplied, nil
}

func (robot *Robot) BatteryHealth() BatteryHealth {
	return robot.batteryHealth
}

func (robot *Robot) ConnectivityAt(now time.Time) Connectivity {
	if robot.latest == nil {
		return ConnectivityUnknown
	}

	if now.Sub(robot.latest.ReceivedAt) <= OnlineWindow {
		return ConnectivityOnline
	}

	return ConnectivityStale
}

func (robot *Robot) validate() error {
	if isValidRobotID(robot.id) == false {
		return fmt.Errorf(
			"%w: id must be 1 to %d letters, digits, '-', '_' or '.'",
			ErrInvalidRobot,
			maxRobotIDLength,
		)
	}

	if robot.siteID == "" {
		return fmt.Errorf("%w: site id is required", ErrInvalidRobot)
	}

	if robot.lifecycle != LifecycleActive && robot.lifecycle != LifecycleInactive {
		return fmt.Errorf("%w: unknown lifecycle %q", ErrInvalidRobot, robot.lifecycle)
	}

	if robot.batteryHealth.isValid() == false {
		return fmt.Errorf("%w: unknown battery health %q", ErrInvalidRobot, robot.batteryHealth)
	}

	if robot.registeredAt.IsZero() || robot.activatedAt.IsZero() {
		return fmt.Errorf("%w: registration and activation times are required", ErrInvalidRobot)
	}

	if robot.activatedAt.Before(robot.registeredAt) {
		return fmt.Errorf("%w: activation time is before registration", ErrInvalidRobot)
	}

	if robot.latest == nil {
		if robot.batteryHealth != BatteryHealthUnknown {
			return fmt.Errorf("%w: battery health is %s with no applied observation", ErrInvalidRobot, robot.batteryHealth)
		}

		return nil
	}

	if err := robot.latest.Observation.validate(); err != nil {
		return fmt.Errorf("%w: latest observation: %w", ErrInvalidRobot, err)
	}

	if robot.latest.ReceivedAt.IsZero() {
		return fmt.Errorf("%w: latest observation has no receipt time", ErrInvalidRobot)
	}

	if robot.batteryHealth.allows(robot.latest.Observation.Battery()) == false {
		return fmt.Errorf(
			"%w: battery health %s does not fit the latest battery %s%%",
			ErrInvalidRobot,
			robot.batteryHealth,
			robot.latest.Observation.Battery(),
		)
	}

	return nil
}

// Robot IDs appear in URL paths.
func isValidRobotID(id RobotID) bool {
	if len(id) == 0 || len(id) > maxRobotIDLength {
		return false
	}

	for _, char := range id {
		isLetter := (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z')
		isDigit := char >= '0' && char <= '9'
		if isLetter == false && isDigit == false && char != '-' && char != '_' && char != '.' {
			return false
		}
	}

	return true
}
