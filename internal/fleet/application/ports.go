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
	WithLockedRobot(ctx context.Context, id domain.RobotID, fn func(robot *domain.Robot, tx RobotTx) error) error
}

// RobotTx is what may be done in the transaction that holds the robot's lock.
type RobotTx interface {
	FindObservation(ctx context.Context, id domain.ObservationID) (domain.Observation, bool, error)
	AddObservation(ctx context.Context, observation domain.Observation, receivedAt time.Time, outcome domain.ObservationOutcome) error
	CloseUnresolvedAlerts(ctx context.Context, reason domain.ClosureReason, now time.Time) (int, error)
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
	Closed   int
}

type AlertRepository interface {
	List(ctx context.Context, terminal bool, limit int) ([]*domain.Alert, error)

	// WithLockedAlert runs fn while the alert row is locked and saves the alert's changes with it.
	WithLockedAlert(ctx context.Context, id domain.AlertID, fn func(alert *domain.Alert) error) error
}

type FleetReadRepository interface {
	Summary(ctx context.Context, siteID *domain.SiteID, onlineSince time.Time) (FleetSummary, error)

	// ObservationHistory returns ErrRobotNotFound for an unknown robot.
	ObservationHistory(ctx context.Context, robotID domain.RobotID, limit int) ([]ObservationRecord, error)
}
