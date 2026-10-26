//go:build integration

package postgres_test

import (
	"context"
	"strings"
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

func TestRobotRepositoryHasNoLatestObservationBeforeTheFirstReport(t *testing.T) {
	ctx := context.Background()
	repository := postgres.NewRobotRepository(startPostgres(t))
	require.NoError(t, repository.Create(ctx, newRobot(t, "robot-101", "site-north")))

	robot, err := repository.FindByID(ctx, "robot-101")
	require.NoError(t, err)

	_, ok := robot.LatestObservation()
	assert.False(t, ok)
}

func TestRobotRepositoryLoadsTheNewestAppliedObservation(t *testing.T) {
	ctx := context.Background()
	repository := postgres.NewRobotRepository(startPostgres(t))
	require.NoError(t, repository.Create(ctx, newRobot(t, "robot-101", "site-north")))

	newer := applied(t, "obs-2", registeredAt.Add(2*time.Second), 18.5, registeredAt.Add(time.Minute))
	older := applied(t, "obs-1", registeredAt.Add(time.Second), 60, registeredAt.Add(2*time.Minute))
	// Inserted newest first, so "latest" cannot mean "last inserted".
	require.NoError(t, repository.AddAppliedObservation(ctx, "robot-101", newer))
	require.NoError(t, repository.AddAppliedObservation(ctx, "robot-101", older))

	robot, err := repository.FindByID(ctx, "robot-101")
	require.NoError(t, err)

	latest, ok := robot.LatestObservation()
	require.True(t, ok)
	assertSameObservation(t, newer, latest)
}

func TestRobotRepositoryListsRobotsWithTheirLatestObservation(t *testing.T) {
	ctx := context.Background()
	repository := postgres.NewRobotRepository(startPostgres(t))
	require.NoError(t, repository.Create(ctx, newRobot(t, "robot-101", "site-north")))
	require.NoError(t, repository.Create(ctx, newRobot(t, "robot-102", "site-north")))
	reported := applied(t, "obs-1", registeredAt.Add(time.Second), 42.25, registeredAt.Add(time.Minute))
	require.NoError(t, repository.AddAppliedObservation(ctx, "robot-101", reported))

	robots, err := repository.List(ctx, nil)
	require.NoError(t, err)
	require.Len(t, robots, 2)

	latest, ok := robots[0].LatestObservation()
	require.True(t, ok)
	assertSameObservation(t, reported, latest)

	_, ok = robots[1].LatestObservation()
	assert.False(t, ok)
}

// Tiny tables favour a sequential scan, so it is switched off to test the query shape.
func TestRobotQueriesUseThePartialIndexForTheCurrentObservation(t *testing.T) {
	ctx := context.Background()
	db := startPostgres(t)

	connection, err := db.Conn(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = connection.Close() })
	_, err = connection.ExecContext(ctx, `SET enable_seqscan = off`)
	require.NoError(t, err)

	queries := map[string]struct {
		query string
		arg   any
	}{
		"find by id": {postgres.FindRobotByIDQuery, "robot-101"},
		"list":       {postgres.ListRobotsQuery, nil},
	}

	for name, tt := range queries {
		t.Run(name, func(t *testing.T) {
			rows, err := connection.QueryContext(ctx, "EXPLAIN "+tt.query, tt.arg)
			require.NoError(t, err)
			defer rows.Close()

			var plan strings.Builder
			for rows.Next() {
				var line string
				require.NoError(t, rows.Scan(&line))
				plan.WriteString(line + "\n")
			}
			require.NoError(t, rows.Err())

			assert.Contains(t, plan.String(), "telemetry_observations_current_idx", plan.String())
			assert.NotContains(t, plan.String(), "Seq Scan on telemetry_observations", plan.String())
		})
	}
}

func applied(
	t *testing.T,
	id domain.ObservationID,
	observedAt time.Time,
	percent float64,
	receivedAt time.Time,
) domain.AppliedObservation {
	t.Helper()

	battery, err := domain.NewBatteryLevel(percent)
	require.NoError(t, err)
	position, err := domain.NewPosition(12.5, -24.8)
	require.NoError(t, err)
	observation, err := domain.NewObservation(id, observedAt, battery, position, domain.OperatingModeCharging)
	require.NoError(t, err)

	return domain.AppliedObservation{Observation: observation, ReceivedAt: receivedAt}
}

func assertSameObservation(t *testing.T, want, got domain.AppliedObservation) {
	t.Helper()

	assert.Equal(t, want.Observation.ID(), got.Observation.ID())
	assert.True(t, want.Observation.ObservedAt().Equal(got.Observation.ObservedAt()))
	assert.True(t, want.ReceivedAt.Equal(got.ReceivedAt))
	assert.Equal(t, want.Observation.Battery(), got.Observation.Battery())
	assert.Equal(t, want.Observation.Position(), got.Observation.Position())
	assert.Equal(t, want.Observation.OperatingMode(), got.Observation.OperatingMode())
}
