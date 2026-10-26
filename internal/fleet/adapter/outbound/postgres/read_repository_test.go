//go:build integration

package postgres_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/adapter/outbound/postgres"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/application"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

var summaryAt = registeredAt.Add(time.Hour)

func TestFleetSummaryCountsActiveRobotsOnlyForAttention(t *testing.T) {
	ctx := context.Background()
	db := seedSummaryFleet(t)
	reads := postgres.NewReadRepository(db)

	all, err := reads.Summary(ctx, nil, summaryAt.Add(-domain.OnlineWindow))
	require.NoError(t, err)
	assert.Equal(t, application.FleetSummary{
		Registered:       7,
		Active:           6,
		Inactive:         1,
		Online:           3,
		Stale:            2,
		LowBattery:       1,
		UnresolvedAlerts: 1,
	}, all)
	assert.Less(t, all.Online+all.Stale, all.Active, "an active robot without a reading is neither online nor stale")

	south := domain.SiteID("site-south")
	atSouth, err := reads.Summary(ctx, &south, summaryAt.Add(-domain.OnlineWindow))
	require.NoError(t, err)
	assert.Equal(t, application.FleetSummary{Registered: 2, Active: 1, Inactive: 1, Stale: 1}, atSouth)

	nowhere := domain.SiteID("site-nowhere")
	none, err := reads.Summary(ctx, &nowhere, summaryAt.Add(-domain.OnlineWindow))
	require.NoError(t, err)
	assert.Equal(t, application.FleetSummary{}, none)
}

func TestObservationHistoryIsNewestFirstAndMarksTheOutcome(t *testing.T) {
	ctx := context.Background()
	db := startPostgres(t)
	robots := postgres.NewRobotRepository(db)
	require.NoError(t, robots.Create(ctx, newRobot(t, "robot-101", "site-north")))
	addObservation(t, robots, "robot-101", applied(t, "obs-2", registeredAt.Add(2*time.Second), 40, registeredAt.Add(time.Minute)), domain.ObservationApplied)
	addObservation(t, robots, "robot-101", applied(t, "obs-1", registeredAt.Add(time.Second), 50, registeredAt.Add(2*time.Minute)), domain.ObservationSuperseded)
	addObservation(t, robots, "robot-101", applied(t, "obs-3", registeredAt.Add(3*time.Second), 30.5, registeredAt.Add(3*time.Minute)), domain.ObservationApplied)
	reads := postgres.NewReadRepository(db)

	history, err := reads.ObservationHistory(ctx, "robot-101", 50)
	require.NoError(t, err)

	require.Len(t, history, 3)
	assert.Equal(t, []string{"obs-3", "obs-2", "obs-1"}, []string{history[0].ObservationID, history[1].ObservationID, history[2].ObservationID})
	assert.Equal(t, []string{"APPLIED", "APPLIED", "SUPERSEDED"}, []string{history[0].Outcome, history[1].Outcome, history[2].Outcome})
	assert.Equal(t, 30.5, history[0].BatteryPercent)
	assert.True(t, history[2].ReceivedAt.Equal(registeredAt.Add(2*time.Minute)))

	limited, err := reads.ObservationHistory(ctx, "robot-101", 2)
	require.NoError(t, err)
	assert.Len(t, limited, 2)
}

func TestObservationHistoryOfARobotThatHasNotReported(t *testing.T) {
	ctx := context.Background()
	db := startPostgres(t)
	require.NoError(t, postgres.NewRobotRepository(db).Create(ctx, newRobot(t, "robot-101", "site-north")))

	history, err := postgres.NewReadRepository(db).ObservationHistory(ctx, "robot-101", 50)

	require.NoError(t, err)
	assert.Empty(t, history)
}

func TestObservationHistoryOfAnUnknownRobot(t *testing.T) {
	_, err := postgres.NewReadRepository(startPostgres(t)).ObservationHistory(context.Background(), "robot-404", 50)

	require.ErrorIs(t, err, application.ErrRobotNotFound)
}

// site-north: online 5 s ago, online exactly 10 s ago, stale, no reading, LOW and online.
// site-south: stale, inactive LOW with a reading.
func seedSummaryFleet(t *testing.T) *sql.DB {
	t.Helper()

	db := startPostgres(t)
	robots := postgres.NewRobotRepository(db)
	report := func(id domain.RobotID, site domain.SiteID, receivedBefore time.Duration) {
		require.NoError(t, robots.Create(context.Background(), newRobot(t, id, site)))
		if receivedBefore >= 0 {
			reading := applied(t, "obs-1", registeredAt.Add(time.Second), 50, summaryAt.Add(-receivedBefore))
			addObservation(t, robots, id, reading, domain.ObservationApplied)
		}
	}

	report("north-online", "site-north", 5*time.Second)
	report("north-boundary", "site-north", 10*time.Second)
	report("north-stale", "site-north", 20*time.Second)
	report("north-silent", "site-north", -1)
	report("north-low", "site-north", time.Second)
	report("south-stale", "site-south", time.Minute)
	report("south-inactive", "site-south", time.Second)

	_, err := db.Exec(`UPDATE robots SET battery_health = 'LOW' WHERE id IN ('north-low', 'south-inactive')`)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE robots SET lifecycle = 'INACTIVE' WHERE id = 'south-inactive'`)
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO alerts (robot_id, kind, status, opened_at, ended_at) VALUES
			('north-low', 'LOW_BATTERY', 'OPEN', $1, NULL),
			('north-stale', 'LOW_BATTERY', 'RESOLVED', $1, $1)`, summaryAt)
	require.NoError(t, err)

	return db
}
