package application_test

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/application"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

type fakeRobots struct {
	stored       map[domain.RobotID]*domain.Robot
	createErr    error
	createCalls  int
	listedSiteID *domain.SiteID
	added        []recordedObservation
	closedAlerts []closedAlerts
	mutex        sync.Mutex
}

type closedAlerts struct {
	robotID domain.RobotID
	reason  domain.ClosureReason
	at      time.Time
}

type recordedObservation struct {
	robotID     domain.RobotID
	observation domain.Observation
	receivedAt  time.Time
	outcome     domain.ObservationOutcome
}

func newFakeRobots() *fakeRobots {
	return &fakeRobots{stored: map[domain.RobotID]*domain.Robot{}}
}

func (robots *fakeRobots) store(
	t *testing.T,
	id domain.RobotID,
	siteID domain.SiteID,
	lifecycle domain.Lifecycle,
	registeredAt time.Time,
) {
	t.Helper()

	robot, err := domain.ReconstituteRobot(id, siteID, lifecycle, registeredAt, registeredAt, domain.BatteryHealthUnknown, nil)
	require.NoError(t, err)
	robots.stored[id] = robot
}

func (robots *fakeRobots) Create(_ context.Context, robot *domain.Robot) error {
	robots.createCalls++
	if robots.createErr != nil {
		return robots.createErr
	}
	if _, exists := robots.stored[robot.ID()]; exists {
		return application.ErrRobotAlreadyExists
	}

	robots.stored[robot.ID()] = robot

	return nil
}

func (robots *fakeRobots) FindByID(_ context.Context, id domain.RobotID) (*domain.Robot, error) {
	robot, exists := robots.stored[id]
	if exists == false {
		return nil, application.ErrRobotNotFound
	}

	return robot, nil
}

func (robots *fakeRobots) List(_ context.Context, siteID *domain.SiteID) ([]*domain.Robot, error) {
	robots.listedSiteID = siteID

	listed := make([]*domain.Robot, 0)
	for _, robot := range robots.stored {
		if siteID == nil || robot.SiteID() == *siteID {
			listed = append(listed, robot)
		}
	}
	sort.Slice(listed, func(i, j int) bool { return listed[i].ID() < listed[j].ID() })

	return listed, nil
}

func (robots *fakeRobots) WithLockedRobot(
	ctx context.Context,
	id domain.RobotID,
	fn func(robot *domain.Robot, tx application.RobotTx) error,
) error {
	robots.mutex.Lock()
	defer robots.mutex.Unlock()

	robot, err := robots.FindByID(ctx, id)
	if err != nil {
		return err
	}

	tx := &fakeRobotTx{robotID: id, robots: robots}
	if err := fn(robot, tx); err != nil {
		return err
	}

	robots.added = append(robots.added, tx.pending...)
	robots.closedAlerts = append(robots.closedAlerts, tx.closed...)

	return nil
}

type fakeRobotTx struct {
	robotID domain.RobotID
	robots  *fakeRobots
	pending []recordedObservation
	closed  []closedAlerts
}

func (log *fakeRobotTx) FindObservation(_ context.Context, id domain.ObservationID) (domain.Observation, bool, error) {
	for _, recorded := range log.robots.added {
		if recorded.robotID == log.robotID && recorded.observation.ID() == id {
			return recorded.observation, true, nil
		}
	}

	return domain.Observation{}, false, nil
}

func (log *fakeRobotTx) AddObservation(
	_ context.Context,
	observation domain.Observation,
	receivedAt time.Time,
	outcome domain.ObservationOutcome,
) error {
	log.pending = append(log.pending, recordedObservation{
		robotID:     log.robotID,
		observation: observation,
		receivedAt:  receivedAt,
		outcome:     outcome,
	})

	return nil
}

func (log *fakeRobotTx) CloseUnresolvedAlerts(_ context.Context, reason domain.ClosureReason, now time.Time) (int, error) {
	log.closed = append(log.closed, closedAlerts{robotID: log.robotID, reason: reason, at: now})

	return 1, nil
}
