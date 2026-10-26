package httpapi_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/adapter/inbound/httpapi"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/application"
)

func TestFleetSummaryForTheWholeFleet(t *testing.T) {
	useCase := &fakeFleetSummary{summary: application.FleetSummary{
		Registered: 7, Active: 6, Inactive: 1, Online: 3, Stale: 2, LowBattery: 1, UnresolvedAlerts: 1,
	}}

	response := serve(httpapi.Config{GetFleetSummary: useCase}, http.MethodGet, "/api/fleet/summary", "")

	require.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, application.GetFleetSummaryQuery{}, useCase.query)
	assert.JSONEq(t, `{
		"site_id": null,
		"registered": 7,
		"active": 6,
		"inactive": 1,
		"online": 3,
		"stale": 2,
		"low_battery": 1,
		"unresolved_alerts": 1
	}`, response.Body.String())
}

func TestFleetSummaryForOneSite(t *testing.T) {
	useCase := &fakeFleetSummary{}

	response := serve(httpapi.Config{GetFleetSummary: useCase}, http.MethodGet, "/api/fleet/summary?site_id=site-north", "")

	require.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, application.GetFleetSummaryQuery{SiteID: "site-north"}, useCase.query)
	var body map[string]any
	decodeBody(t, response.Body.Bytes(), &body)
	assert.Equal(t, "site-north", body["site_id"])
}

func TestObservationHistoryShowsEachObservationWithItsOutcome(t *testing.T) {
	observedAt := time.Date(2026, time.October, 25, 14, 5, 0, 0, time.UTC)
	useCase := &fakeObservationHistory{records: []application.ObservationRecord{{
		ObservationID:  "obs-1",
		ObservedAt:     observedAt,
		ReceivedAt:     observedAt.Add(time.Second),
		BatteryPercent: 18.5,
		PositionX:      12.5,
		PositionY:      24.8,
		OperatingMode:  "WORKING",
		Outcome:        "SUPERSEDED",
	}}}

	response := serve(httpapi.Config{GetObservationHistory: useCase}, http.MethodGet, "/api/robots/robot-101/telemetry?limit=5", "")

	require.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, application.GetObservationHistoryQuery{RobotID: "robot-101", Limit: 5}, useCase.query)
	assert.JSONEq(t, `{
		"robot_id": "robot-101",
		"observations": [{
			"observation_id": "obs-1",
			"observed_at": "2026-10-25T14:05:00Z",
			"received_at": "2026-10-25T14:05:01Z",
			"battery_percent": 18.5,
			"position": {"x": 12.5, "y": 24.8},
			"operating_mode": "WORKING",
			"outcome": "SUPERSEDED"
		}]
	}`, response.Body.String())
}

func TestObservationHistoryOfARobotThatHasNotReported(t *testing.T) {
	response := serve(httpapi.Config{GetObservationHistory: &fakeObservationHistory{}}, http.MethodGet, "/api/robots/robot-101/telemetry", "")

	require.Equal(t, http.StatusOK, response.Code)
	assert.JSONEq(t, `{"robot_id": "robot-101", "observations": []}`, response.Body.String())
}

func TestObservationHistoryErrors(t *testing.T) {
	badLimit := serve(httpapi.Config{GetObservationHistory: &fakeObservationHistory{}}, http.MethodGet, "/api/robots/robot-101/telemetry?limit=0", "")
	assert.Equal(t, http.StatusBadRequest, badLimit.Code)
	assert.Equal(t, "INVALID_REQUEST", errorCode(t, badLimit))

	unknown := serve(
		httpapi.Config{GetObservationHistory: &fakeObservationHistory{err: application.ErrRobotNotFound}},
		http.MethodGet, "/api/robots/robot-404/telemetry", "",
	)
	assert.Equal(t, http.StatusNotFound, unknown.Code)
	assert.Equal(t, "ROBOT_NOT_FOUND", errorCode(t, unknown))
}

type fakeFleetSummary struct {
	summary application.FleetSummary
	query   application.GetFleetSummaryQuery
}

func (useCase *fakeFleetSummary) Execute(_ context.Context, query application.GetFleetSummaryQuery) (application.FleetSummary, error) {
	useCase.query = query

	return useCase.summary, nil
}

type fakeObservationHistory struct {
	records []application.ObservationRecord
	err     error
	query   application.GetObservationHistoryQuery
}

func (useCase *fakeObservationHistory) Execute(
	_ context.Context,
	query application.GetObservationHistoryQuery,
) ([]application.ObservationRecord, error) {
	useCase.query = query

	return useCase.records, useCase.err
}
