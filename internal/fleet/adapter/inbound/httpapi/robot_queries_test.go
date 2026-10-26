package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/adapter/inbound/httpapi"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/application"
)

func TestGetRobotShowsAnUnreportedRobotWithUnknownsAndNulls(t *testing.T) {
	useCase := &fakeGetRobot{state: testState(t, "robot-101", "site-north")}

	response := serve(httpapi.Config{GetRobot: useCase}, http.MethodGet, "/api/robots/robot-101", "")

	require.Equal(t, http.StatusOK, response.Code)
	assert.JSONEq(t, `{
		"id": "robot-101",
		"site_id": "site-north",
		"lifecycle": "ACTIVE",
		"registered_at": "2026-10-25T14:00:00Z",
		"battery_health": "UNKNOWN",
		"connectivity": "UNKNOWN",
		"latest_observation": null
	}`, response.Body.String())
	assert.Equal(t, "robot-101", useCase.robotID)
}

func TestGetRobotMapsErrors(t *testing.T) {
	tests := map[string]struct {
		err        error
		wantStatus int
		wantCode   string
	}{
		"unknown robot":    {application.ErrRobotNotFound, http.StatusNotFound, "ROBOT_NOT_FOUND"},
		"unexpected fault": {errors.New("connection reset"), http.StatusInternalServerError, "INTERNAL"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			response := serve(httpapi.Config{GetRobot: &fakeGetRobot{err: tt.err}}, http.MethodGet, "/api/robots/robot-404", "")

			assert.Equal(t, tt.wantStatus, response.Code)
			assert.Equal(t, tt.wantCode, errorCode(t, response))
		})
	}
}

func TestListRobotsReturnsEveryRobot(t *testing.T) {
	useCase := &fakeListRobots{states: []application.RobotState{
		testState(t, "robot-101", "site-north"),
		testState(t, "robot-201", "site-south"),
	}}

	response := serve(httpapi.Config{ListRobots: useCase}, http.MethodGet, "/api/robots", "")

	require.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, application.ListRobotsQuery{}, useCase.query)

	var body struct {
		Robots []map[string]any `json:"robots"`
	}
	decodeBody(t, response.Body.Bytes(), &body)
	require.Len(t, body.Robots, 2)
	assert.Equal(t, "robot-101", body.Robots[0]["id"])
	assert.Equal(t, "robot-201", body.Robots[1]["id"])
}

func TestListRobotsPassesTheSiteFilter(t *testing.T) {
	useCase := &fakeListRobots{}

	serve(httpapi.Config{ListRobots: useCase}, http.MethodGet, "/api/robots?site_id=site-north", "")

	assert.Equal(t, application.ListRobotsQuery{SiteID: "site-north"}, useCase.query)
}

func TestListRobotsReturnsAnEmptyArrayNotNull(t *testing.T) {
	response := serve(httpapi.Config{ListRobots: &fakeListRobots{}}, http.MethodGet, "/api/robots?site_id=site-empty", "")

	require.Equal(t, http.StatusOK, response.Code)
	assert.JSONEq(t, `{"robots": []}`, response.Body.String())
}

type fakeGetRobot struct {
	state   application.RobotState
	err     error
	robotID string
}

func (useCase *fakeGetRobot) Execute(_ context.Context, robotID string) (application.RobotState, error) {
	useCase.robotID = robotID

	return useCase.state, useCase.err
}

type fakeListRobots struct {
	states []application.RobotState
	query  application.ListRobotsQuery
}

func (useCase *fakeListRobots) Execute(
	_ context.Context,
	query application.ListRobotsQuery,
) ([]application.RobotState, error) {
	useCase.query = query

	return useCase.states, nil
}
