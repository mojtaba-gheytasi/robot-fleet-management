package application

import (
	"context"
	"time"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

type RobotRepository interface {
	// Create stores a new robot. It returns ErrRobotAlreadyExists when the ID
	// is taken, leaving the stored robot unchanged, and ErrSiteNotFound when
	// the site does not exist.
	Create(ctx context.Context, robot *domain.Robot) error
	FindByID(ctx context.Context, id domain.RobotID) (*domain.Robot, error)

	// List returns robots ordered by ID; a nil siteID means every site.
	List(ctx context.Context, siteID *domain.SiteID) ([]*domain.Robot, error)

	// AddAppliedObservation records an observation that became the robot's
	// current state. History and current state are the same rows, so this
	// one insert updates both.
	AddAppliedObservation(ctx context.Context, robotID domain.RobotID, applied domain.AppliedObservation) error
}

type Clock func() time.Time
