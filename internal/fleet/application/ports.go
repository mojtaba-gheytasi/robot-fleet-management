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
}

type Clock func() time.Time
