package domain

import (
	"fmt"
	"time"
)

const (
	maxRobotIDLength = 64

	// A robot is ONLINE while its last applied observation was received no
	// more than this long ago (HLT-04).
	onlineWindow = 10 * time.Second
)

type RobotID string
type SiteID string

type Lifecycle string

const (
	LifecycleActive   Lifecycle = "ACTIVE"
	LifecycleInactive Lifecycle = "INACTIVE"
)

type BatteryHealth string

const BatteryHealthUnknown BatteryHealth = "UNKNOWN"

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

	// latest is the current state: the newest applied observation, or nil
	// before the robot has reported. History is stored separately and is not
	// part of the aggregate, so it stays small however long the robot runs.
	latest *AppliedObservation
}

// NewRobot registers a robot at a site. A new robot starts ACTIVE (REG-03).
// Whether the site exists is checked when the robot is stored.
func NewRobot(id RobotID, siteID SiteID, now time.Time) (*Robot, error) {
	robot := &Robot{
		id:           id,
		siteID:       siteID,
		lifecycle:    LifecycleActive,
		registeredAt: now,
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
	latest *AppliedObservation,
) (*Robot, error) {
	robot := &Robot{
		id:           id,
		siteID:       siteID,
		lifecycle:    lifecycle,
		registeredAt: registeredAt,
		latest:       latest,
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

// LatestObservation returns the current state, and false before the robot
// has reported.
func (robot *Robot) LatestObservation() (AppliedObservation, bool) {
	if robot.latest == nil {
		return AppliedObservation{}, false
	}

	return *robot.latest, true
}

// ApplyObservation makes the observation the robot's current state.
func (robot *Robot) ApplyObservation(observation Observation, receivedAt time.Time) AppliedObservation {
	applied := AppliedObservation{Observation: observation, ReceivedAt: receivedAt}
	robot.latest = &applied

	return applied
}

// BatteryHealth stays UNKNOWN until battery health is classified from applied
// observations (HLT-01 to HLT-03).
func (robot *Robot) BatteryHealth() BatteryHealth {
	return BatteryHealthUnknown
}

// ConnectivityAt takes the time of reading because connectivity is never
// stored: the same robot is ONLINE now and STALE ten seconds later without
// any write. It uses the server's receipt time, not the robot's clock.
func (robot *Robot) ConnectivityAt(now time.Time) Connectivity {
	if robot.latest == nil {
		return ConnectivityUnknown
	}

	if now.Sub(robot.latest.ReceivedAt) <= onlineWindow {
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

	if robot.registeredAt.IsZero() {
		return fmt.Errorf("%w: registration time is required", ErrInvalidRobot)
	}

	return nil
}

// The ID appears in URL paths such as /api/robots/{id}, so characters like
// '/' or spaces would make a robot impossible to address.
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
