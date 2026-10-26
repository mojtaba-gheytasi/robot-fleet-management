package application_test

import (
	"context"
	"sort"
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
	added        []domain.AppliedObservation
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

func (robots *fakeRobots) AddAppliedObservation(
	_ context.Context,
	robotID domain.RobotID,
	applied domain.AppliedObservation,
) error {
	robots.added = append(robots.added, applied)

	stored := robots.stored[robotID]
	stored.ApplyObservation(applied.Observation, applied.ReceivedAt)

	return nil
}
