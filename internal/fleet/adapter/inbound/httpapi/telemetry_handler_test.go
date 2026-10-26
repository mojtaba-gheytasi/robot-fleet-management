package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/adapter/inbound/httpapi"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/application"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

const validTelemetryBody = `{
	"observation_id": "obs-102",
	"observed_at": "2026-10-25T14:05:00Z",
	"battery_percent": 18.50,
	"position": {"x": 12.5, "y": 24.8},
	"operating_mode": "WORKING"
}`

func TestRecordTelemetryReturnsTheOutcome(t *testing.T) {
	useCase := &fakeRecordTelemetry{result: application.RecordTelemetryResult{Outcome: application.TelemetryOutcomeApplied}}

	response := postTelemetry(useCase, validTelemetryBody)

	require.Equal(t, http.StatusOK, response.Code)
	assert.JSONEq(t, `{"outcome": "applied", "robot_id": "robot-101", "observation_id": "obs-102"}`, response.Body.String())
	assert.Equal(t, application.RecordTelemetryCommand{
		RobotID:        "robot-101",
		ObservationID:  "obs-102",
		ObservedAt:     time.Date(2026, time.October, 25, 14, 5, 0, 0, time.UTC),
		BatteryPercent: 18.5,
		PositionX:      12.5,
		PositionY:      24.8,
		OperatingMode:  "WORKING",
	}, useCase.command)
}

func TestRecordTelemetryReturns200ForEveryOutcome(t *testing.T) {
	outcomes := []application.TelemetryOutcome{
		application.TelemetryOutcomeApplied,
		application.TelemetryOutcomeSuperseded,
		application.TelemetryOutcomeDuplicate,
	}

	for _, outcome := range outcomes {
		t.Run(string(outcome), func(t *testing.T) {
			useCase := &fakeRecordTelemetry{result: application.RecordTelemetryResult{Outcome: outcome}}

			response := postTelemetry(useCase, validTelemetryBody)

			require.Equal(t, http.StatusOK, response.Code)
			assert.JSONEq(t,
				`{"outcome": "`+string(outcome)+`", "robot_id": "robot-101", "observation_id": "obs-102"}`,
				response.Body.String(),
			)
		})
	}
}

func TestRecordTelemetryRejectsMissingFields(t *testing.T) {
	tests := map[string]string{
		"empty object":         `{}`,
		"no battery":           `{"observation_id": "o", "observed_at": "2026-10-25T14:05:00Z", "position": {"x": 1, "y": 2}, "operating_mode": "IDLE"}`,
		"no position":          `{"observation_id": "o", "observed_at": "2026-10-25T14:05:00Z", "battery_percent": 50, "operating_mode": "IDLE"}`,
		"position without y":   `{"observation_id": "o", "observed_at": "2026-10-25T14:05:00Z", "battery_percent": 50, "position": {"x": 1}, "operating_mode": "IDLE"}`,
		"not an RFC 3339 time": `{"observation_id": "o", "observed_at": "25/10/2026", "battery_percent": 50, "position": {"x": 1, "y": 2}, "operating_mode": "IDLE"}`,
	}

	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			useCase := &fakeRecordTelemetry{}

			response := postTelemetry(useCase, body)

			assert.Equal(t, http.StatusBadRequest, response.Code)
			assert.Equal(t, "INVALID_REQUEST", errorCode(t, response))
			assert.False(t, useCase.called)
		})
	}
}

func TestRecordTelemetryMapsUseCaseErrors(t *testing.T) {
	tests := map[string]struct {
		err        error
		wantStatus int
		wantCode   string
	}{
		"invalid observation": {domain.ErrInvalidObservation, http.StatusUnprocessableEntity, "INVALID_OBSERVATION"},
		"reused id":           {domain.ErrObservationConflict, http.StatusConflict, "OBSERVATION_CONFLICT"},
		"clock ahead":         {domain.ErrClockSkewExceeded, http.StatusUnprocessableEntity, "CLOCK_SKEW_EXCEEDED"},
		"unknown robot":       {application.ErrRobotNotFound, http.StatusNotFound, "ROBOT_NOT_FOUND"},
		"unexpected fault":    {errors.New("connection reset"), http.StatusInternalServerError, "INTERNAL"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			response := postTelemetry(&fakeRecordTelemetry{err: tt.err}, validTelemetryBody)

			assert.Equal(t, tt.wantStatus, response.Code)
			assert.Equal(t, tt.wantCode, errorCode(t, response))
		})
	}
}

func TestGetRobotShowsTheLatestObservation(t *testing.T) {
	state := testState(t, "robot-101", "site-north")
	battery, err := domain.NewBatteryLevel(18.5)
	require.NoError(t, err)
	position, err := domain.NewPosition(12.5, 24.8)
	require.NoError(t, err)
	observation, err := domain.NewObservation(
		"obs-102",
		time.Date(2026, time.October, 25, 14, 5, 0, 0, time.UTC),
		battery,
		position,
		domain.OperatingModeWorking,
	)
	require.NoError(t, err)
	_, err = state.Robot.ApplyObservation(observation, time.Date(2026, time.October, 25, 14, 5, 1, 0, time.UTC))
	require.NoError(t, err)
	state.Connectivity = domain.ConnectivityOnline

	response := serve(httpapi.Config{GetRobot: &fakeGetRobot{state: state}}, http.MethodGet, "/api/robots/robot-101", "")

	require.Equal(t, http.StatusOK, response.Code)
	assert.JSONEq(t, `{
		"id": "robot-101",
		"site_id": "site-north",
		"lifecycle": "ACTIVE",
		"registered_at": "2026-10-25T14:00:00Z",
		"battery_health": "LOW",
		"connectivity": "ONLINE",
		"latest_observation": {
			"observation_id": "obs-102",
			"observed_at": "2026-10-25T14:05:00Z",
			"received_at": "2026-10-25T14:05:01Z",
			"battery_percent": 18.5,
			"position": {"x": 12.5, "y": 24.8},
			"operating_mode": "WORKING"
		}
	}`, response.Body.String())
}

func postTelemetry(useCase *fakeRecordTelemetry, body string) *httptest.ResponseRecorder {
	return serve(httpapi.Config{RecordTelemetry: useCase}, http.MethodPost, "/api/robots/robot-101/telemetry", body)
}

type fakeRecordTelemetry struct {
	result  application.RecordTelemetryResult
	err     error
	called  bool
	command application.RecordTelemetryCommand
}

func (useCase *fakeRecordTelemetry) Execute(
	_ context.Context,
	command application.RecordTelemetryCommand,
) (application.RecordTelemetryResult, error) {
	useCase.called = true
	useCase.command = command

	return useCase.result, useCase.err
}
