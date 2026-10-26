package domain

import (
	"fmt"
	"time"
)

const maxRobotIDLength = 64

type RobotID string
type SiteID string

type Lifecycle string

const (
	LifecycleActive   Lifecycle = "ACTIVE"
	LifecycleInactive Lifecycle = "INACTIVE"
)

type Robot struct {
	id           RobotID
	siteID       SiteID
	lifecycle    Lifecycle
	registeredAt time.Time
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
) (*Robot, error) {
	robot := &Robot{
		id:           id,
		siteID:       siteID,
		lifecycle:    lifecycle,
		registeredAt: registeredAt,
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
