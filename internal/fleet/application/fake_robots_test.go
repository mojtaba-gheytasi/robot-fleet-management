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
	mutex        sync.Mutex
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

	robot, err := domain.ReconstituteRobot(id, siteID, lifecycle, registeredAt, nil)
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
	fn func(robot *domain.Robot, log application.ObservationLog) error,
) error {
	robots.mutex.Lock()
	defer robots.mutex.Unlock()

	robot, err := robots.FindByID(ctx, id)
	if err != nil {
		return err
	}

	log := &fakeObservationLog{robotID: id, robots: robots}
	if err := fn(robot, log); err != nil {
		return err
	}

	robots.added = append(robots.added, log.pending...)

	return nil
}

type fakeObservationLog struct {
	robotID domain.RobotID
	robots  *fakeRobots
	pending []recordedObservation
}

func (log *fakeObservationLog) Find(_ context.Context, id domain.ObservationID) (domain.Observation, bool, error) {
	for _, recorded := range log.robots.added {
		if recorded.robotID == log.robotID && recorded.observation.ID() == id {
			return recorded.observation, true, nil
		}
	}

	return domain.Observation{}, false, nil
}

func (log *fakeObservationLog) Add(
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
