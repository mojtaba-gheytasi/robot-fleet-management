package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/adapter/inbound/httpapi"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/application"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

const validBody = `{"id": "robot-101", "site_id": "site-north"}`

func TestRegisterRobotReturns201WithTheNewRobot(t *testing.T) {
	useCase := &fakeRegisterRobot{result: application.RegisterRobotResult{RobotState: testState(t, "robot-101", "site-north"), Created: true}}

	response := postRobot(t, useCase, validBody)

	require.Equal(t, http.StatusCreated, response.Code)
	assert.Equal(t, "application/json", response.Header().Get("Content-Type"))
	assert.JSONEq(t, `{
		"id": "robot-101",
		"site_id": "site-north",
		"lifecycle": "ACTIVE",
		"registered_at": "2026-10-25T14:00:00Z",
		"battery_health": "UNKNOWN",
		"connectivity": "UNKNOWN",
		"latest_observation": null
	}`, response.Body.String())
	assert.Equal(t, application.RegisterRobotCommand{RobotID: "robot-101", SiteID: "site-north"}, useCase.command)
}

func TestRegisterRobotReturns200OnIdenticalRetry(t *testing.T) {
	useCase := &fakeRegisterRobot{result: application.RegisterRobotResult{RobotState: testState(t, "robot-101", "site-north"), Created: false}}

	response := postRobot(t, useCase, validBody)

	assert.Equal(t, http.StatusOK, response.Code)
}

func TestRegisterRobotMapsUseCaseErrors(t *testing.T) {
	tests := map[string]struct {
		err        error
		wantStatus int
		wantCode   string
	}{
		"invalid robot":    {domain.ErrInvalidRobot, http.StatusBadRequest, "INVALID_REQUEST"},
		"unknown site":     {application.ErrSiteNotFound, http.StatusNotFound, "SITE_NOT_FOUND"},
		"different data":   {application.ErrRobotRegistrationConflict, http.StatusConflict, "ROBOT_REGISTRATION_CONFLICT"},
		"unexpected fault": {errors.New("connection reset"), http.StatusInternalServerError, "INTERNAL"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			response := postRobot(t, &fakeRegisterRobot{err: tt.err}, validBody)

			assert.Equal(t, tt.wantStatus, response.Code)
			assert.Equal(t, tt.wantCode, errorCode(t, response))
		})
	}
}

func TestRegisterRobotDoesNotExposeInternalErrors(t *testing.T) {
	response := postRobot(t, &fakeRegisterRobot{err: errors.New("connection reset")}, validBody)

	assert.NotContains(t, response.Body.String(), "connection reset")
}

func TestRegisterRobotRejectsMalformedBodies(t *testing.T) {
	tests := map[string]string{
		"not JSON":        `robot-101`,
		"unknown field":   `{"id": "robot-101", "site_id": "site-north", "name": "Rob"}`,
		"wrong type":      `{"id": 101, "site_id": "site-north"}`,
		"trailing data":   validBody + `{}`,
		"empty body":      ``,
		"body over limit": `{"id": "` + strings.Repeat("r", 70<<10) + `"}`,
	}

	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			useCase := &fakeRegisterRobot{}

			response := postRobot(t, useCase, body)

			assert.Equal(t, http.StatusBadRequest, response.Code)
			assert.Equal(t, "INVALID_REQUEST", errorCode(t, response))
			assert.False(t, useCase.called, "a malformed body never reaches the use case")
		})
	}
}

func postRobot(t *testing.T, useCase *fakeRegisterRobot, body string) *httptest.ResponseRecorder {
	t.Helper()

	return serve(httpapi.Config{RegisterRobot: useCase}, http.MethodPost, "/api/robots", body)
}

func serve(config httpapi.Config, method, target, body string) *httptest.ResponseRecorder {
	config.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))

	response := httptest.NewRecorder()
	httpapi.NewHandler(config).ServeHTTP(response, httptest.NewRequest(method, target, strings.NewReader(body)))

	return response
}

func errorCode(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decodeBody(t, response.Body.Bytes(), &body)

	return body.Error.Code
}

func decodeBody(t *testing.T, data []byte, target any) {
	t.Helper()

	require.NoError(t, json.Unmarshal(data, target))
}

func testState(t *testing.T, id domain.RobotID, siteID domain.SiteID) application.RobotState {
	t.Helper()

	robot, err := domain.NewRobot(id, siteID, time.Date(2026, time.October, 25, 14, 0, 0, 0, time.UTC))
	require.NoError(t, err)

	return application.RobotState{Robot: robot, Connectivity: domain.ConnectivityUnknown}
}

type fakeRegisterRobot struct {
	result  application.RegisterRobotResult
	err     error
	called  bool
	command application.RegisterRobotCommand
}

func (useCase *fakeRegisterRobot) Execute(
	_ context.Context,
	command application.RegisterRobotCommand,
) (application.RegisterRobotResult, error) {
	useCase.called = true
	useCase.command = command

	return useCase.result, useCase.err
}

func TestRegisterRobotRejectsIDsTheRouterWouldRewrite(t *testing.T) {
	// The real use case rejects the ID before it reaches the repository, so none is needed.
	registerRobot := application.NewRegisterRobot(nil, time.Now)

	for _, id := range []string{".", ".."} {
		response := serve(httpapi.Config{RegisterRobot: registerRobot}, http.MethodPost, "/api/robots",
			`{"id": "`+id+`", "site_id": "site-north"}`)

		assert.Equal(t, http.StatusBadRequest, response.Code, id)
		assert.Equal(t, "INVALID_REQUEST", errorCode(t, response), id)
	}
}

func TestADottedRobotIDReachesItsTelemetryRoute(t *testing.T) {
	useCase := &fakeRecordTelemetry{result: application.RecordTelemetryResult{Outcome: application.TelemetryOutcomeApplied}}

	response := serve(httpapi.Config{RecordTelemetry: useCase}, http.MethodPost, "/api/robots/robot.1/telemetry", validTelemetryBody)

	require.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, "robot.1", useCase.command.RobotID)
}
