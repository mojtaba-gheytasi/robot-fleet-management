package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/application"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

func TestFleetSummaryCountsRobotsOnlineWithinTenSeconds(t *testing.T) {
	fleet := &fakeFleetReads{summary: application.FleetSummary{Registered: 3}}

	summary, err := application.NewGetFleetSummary(fleet, clockAt(fixedNow)).Execute(context.Background(), application.GetFleetSummaryQuery{})
	require.NoError(t, err)

	assert.Equal(t, 3, summary.Registered)
	assert.Nil(t, fleet.siteID)
	assert.Equal(t, fixedNow.Add(-10*time.Second), fleet.onlineSince)
}

func TestFleetSummaryForOneSite(t *testing.T) {
	fleet := &fakeFleetReads{}

	_, err := application.NewGetFleetSummary(fleet, clockAt(fixedNow)).
		Execute(context.Background(), application.GetFleetSummaryQuery{SiteID: "site-north"})
	require.NoError(t, err)

	require.NotNil(t, fleet.siteID)
	assert.Equal(t, domain.SiteID("site-north"), *fleet.siteID)
}

func TestObservationHistoryBoundsTheLimit(t *testing.T) {
	for requested, want := range map[int]int{0: 50, 5: 5, 1000: 200} {
		fleet := &fakeFleetReads{}

		_, err := application.NewGetObservationHistory(fleet).
			Execute(context.Background(), application.GetObservationHistoryQuery{RobotID: "robot-101", Limit: requested})
		require.NoError(t, err)

		assert.Equal(t, domain.RobotID("robot-101"), fleet.robotID)
		assert.Equal(t, want, fleet.limit, "requested %d", requested)
	}
}

func TestObservationHistoryOfAnUnknownRobot(t *testing.T) {
	fleet := &fakeFleetReads{historyErr: application.ErrRobotNotFound}

	_, err := application.NewGetObservationHistory(fleet).
		Execute(context.Background(), application.GetObservationHistoryQuery{RobotID: "robot-404"})

	require.ErrorIs(t, err, application.ErrRobotNotFound)
}

type fakeFleetReads struct {
	summary     application.FleetSummary
	historyErr  error
	siteID      *domain.SiteID
	onlineSince time.Time
	robotID     domain.RobotID
	limit       int
}

func (fleet *fakeFleetReads) Summary(_ context.Context, siteID *domain.SiteID, onlineSince time.Time) (application.FleetSummary, error) {
	fleet.siteID = siteID
	fleet.onlineSince = onlineSince

	return fleet.summary, nil
}

func (fleet *fakeFleetReads) ObservationHistory(
	_ context.Context,
	robotID domain.RobotID,
	limit int,
) ([]application.ObservationRecord, error) {
	fleet.robotID = robotID
	fleet.limit = limit

	return nil, fleet.historyErr
}
