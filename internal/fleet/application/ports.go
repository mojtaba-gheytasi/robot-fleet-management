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

	// WithLockedRobot runs fn while the robot row is locked, then saves the robot's lifecycle,
	// activation time and battery health. Observations and alerts are written through tx.
	// Everything commits together; an error from fn rolls everything back.
	WithLockedRobot(ctx context.Context, id domain.RobotID, fn func(robot *domain.Robot, tx RobotTx) error) error
}

// RobotTx is what may be done in the transaction that holds the robot's lock.
// Locks are always taken robot first, then alert, so they cannot deadlock.
type RobotTx interface {
	FindObservation(ctx context.Context, id domain.ObservationID) (domain.Observation, bool, error)
	AddObservation(ctx context.Context, observation domain.Observation, receivedAt time.Time, outcome domain.ObservationOutcome) error

	// FindUnresolvedAlert locks and returns the robot's unresolved alert, or nil if it has none.
	FindUnresolvedAlert(ctx context.Context) (*domain.Alert, error)
	AddAlert(ctx context.Context, alert *domain.Alert) error
	SaveAlert(ctx context.Context, alert *domain.Alert) error
}

type Clock func() time.Time

type AlertRepository interface {
	// List with a limit of 0 returns every matching alert.
	List(ctx context.Context, terminal bool, limit int) ([]*domain.Alert, error)

	// WithLockedAlert runs fn while the alert row is locked and saves the alert's changes with it.
	WithLockedAlert(ctx context.Context, id domain.AlertID, fn func(alert *domain.Alert) error) error
}

type FleetReadRepository interface {
	Summary(ctx context.Context, siteID *domain.SiteID, onlineSince time.Time) (FleetSummary, error)

	// ObservationHistory returns ErrRobotNotFound for an unknown robot.
	ObservationHistory(ctx context.Context, robotID domain.RobotID, limit int) ([]ObservationRecord, error)
}
