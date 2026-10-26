//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/adapter/outbound/postgres"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/application"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

var registeredAt = time.Date(2026, time.October, 25, 14, 0, 0, 0, time.UTC)

func TestRobotRepositoryCreateAndFind(t *testing.T) {
	ctx := context.Background()
	repository := postgres.NewRobotRepository(startPostgres(t))

	created := newRobot(t, "robot-101", "site-north")
	require.NoError(t, repository.Create(ctx, created))

	found, err := repository.FindByID(ctx, "robot-101")
	require.NoError(t, err)

	assert.Equal(t, created.ID(), found.ID())
	assert.Equal(t, created.SiteID(), found.SiteID())
	assert.Equal(t, domain.LifecycleActive, found.Lifecycle())
	assert.True(t, created.RegisteredAt().Equal(found.RegisteredAt()))
}

func TestRobotRepositoryCreateLeavesAnExistingRobotUnchanged(t *testing.T) {
	ctx := context.Background()
	db := startPostgres(t)
	repository := postgres.NewRobotRepository(db)

	require.NoError(t, repository.Create(ctx, newRobot(t, "robot-101", "site-north")))
	_, err := db.ExecContext(ctx, `UPDATE robots SET lifecycle = 'INACTIVE' WHERE id = 'robot-101'`)
	require.NoError(t, err)

	err = repository.Create(ctx, newRobot(t, "robot-101", "site-south"))
	require.ErrorIs(t, err, application.ErrRobotAlreadyExists)

	found, err := repository.FindByID(ctx, "robot-101")
	require.NoError(t, err)
	assert.Equal(t, domain.SiteID("site-north"), found.SiteID())
	assert.Equal(t, domain.LifecycleInactive, found.Lifecycle(), "a repeated create does not reactivate")
}

func TestRobotRepositoryCreateRejectsAnUnknownSite(t *testing.T) {
	ctx := context.Background()
	repository := postgres.NewRobotRepository(startPostgres(t))

	err := repository.Create(ctx, newRobot(t, "robot-101", "site-nowhere"))

	require.ErrorIs(t, err, application.ErrSiteNotFound)
}

func TestRobotRepositoryFindUnknownRobot(t *testing.T) {
	repository := postgres.NewRobotRepository(startPostgres(t))

	_, err := repository.FindByID(context.Background(), "robot-404")

	require.ErrorIs(t, err, application.ErrRobotNotFound)
}

func newRobot(t *testing.T, id domain.RobotID, siteID domain.SiteID) *domain.Robot {
	t.Helper()

	robot, err := domain.NewRobot(id, siteID, registeredAt)
	require.NoError(t, err)

	return robot
}

func TestRobotRepositoryListsRobotsOrderedByID(t *testing.T) {
	ctx := context.Background()
	repository := postgres.NewRobotRepository(startPostgres(t))
	for _, robot := range []*domain.Robot{
		newRobot(t, "robot-201", "site-south"),
		newRobot(t, "robot-102", "site-north"),
		newRobot(t, "robot-101", "site-north"),
	} {
		require.NoError(t, repository.Create(ctx, robot))
	}

	all, err := repository.List(ctx, nil)
	require.NoError(t, err)
	assert.Equal(t, []domain.RobotID{"robot-101", "robot-102", "robot-201"}, robotIDs(all))

	north := domain.SiteID("site-north")
	atNorth, err := repository.List(ctx, &north)
	require.NoError(t, err)
	assert.Equal(t, []domain.RobotID{"robot-101", "robot-102"}, robotIDs(atNorth))
}

func TestRobotRepositoryListsNothingForASiteWithoutRobots(t *testing.T) {
	ctx := context.Background()
	repository := postgres.NewRobotRepository(startPostgres(t))
	require.NoError(t, repository.Create(ctx, newRobot(t, "robot-101", "site-north")))

	south := domain.SiteID("site-south")
	robots, err := repository.List(ctx, &south)

	require.NoError(t, err)
	assert.Empty(t, robots)
}

func robotIDs(robots []*domain.Robot) []domain.RobotID {
	ids := make([]domain.RobotID, 0, len(robots))
	for _, robot := range robots {
		ids = append(ids, robot.ID())
	}

	return ids
}
