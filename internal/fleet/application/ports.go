package application

import (
	"context"
	"time"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

type RobotRepository interface {
	// Create returns ErrRobotAlreadyExists or ErrSiteNotFound.
	Create(ctx context.Context, robot *domain.Robot) error
	FindByID(ctx context.Context, id domain.RobotID) (*domain.Robot, error)

	// List with a nil siteID returns every site.
	List(ctx context.Context, siteID *domain.SiteID) ([]*domain.Robot, error)

	AddAppliedObservation(ctx context.Context, robotID domain.RobotID, applied domain.AppliedObservation) error
}

type Clock func() time.Time
