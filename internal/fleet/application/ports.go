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

	// WithLockedRobot runs fn while the robot row is locked and saves the robot's changes with it;
	// an error from fn rolls everything back.
	WithLockedRobot(ctx context.Context, id domain.RobotID, fn func(robot *domain.Robot, log ObservationLog) error) error
}

type ObservationLog interface {
	Find(ctx context.Context, id domain.ObservationID) (domain.Observation, bool, error)
	Add(ctx context.Context, observation domain.Observation, receivedAt time.Time, outcome domain.ObservationOutcome) error
}

type Clock func() time.Time

type AlertReconciler interface {
	Reconcile(ctx context.Context, now time.Time) (AlertReconciliation, error)
}

type AlertReconciliation struct {
	// Skipped means another copy of the service was already running the check.
	Skipped  bool
	Opened   int
	Resolved int
}
