package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/adapter/inbound/httpapi"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/application"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

func TestListAlertsShowsUnresolvedAlertsByDefault(t *testing.T) {
	openedAt := time.Date(2026, time.October, 25, 15, 0, 0, 0, time.UTC)
	alert, err := domain.ReconstituteAlert(
		"7d2f5c1e-0000-4000-8000-000000000001", "robot-101", domain.AlertKindLowBattery,
		domain.AlertStatusOpen, openedAt, nil, nil, "",
	)
	require.NoError(t, err)
	useCase := &fakeListAlerts{alerts: []*domain.Alert{alert}}

	response := serve(httpapi.Config{ListAlerts: useCase}, http.MethodGet, "/api/alerts", "")

	require.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, application.ListAlertsQuery{}, useCase.query)
	assert.JSONEq(t, `{"alerts": [{
		"id": "7d2f5c1e-0000-4000-8000-000000000001",
		"robot_id": "robot-101",
		"kind": "LOW_BATTERY",
		"status": "OPEN",
		"opened_at": "2026-10-25T15:00:00Z",
		"acknowledged_at": null,
		"ended_at": null,
		"closure_reason": null
	}]}`, response.Body.String())
}

func TestListAlertsShowsATerminalAlertWithItsEnd(t *testing.T) {
	openedAt := time.Date(2026, time.October, 25, 15, 0, 0, 0, time.UTC)
	endedAt := openedAt.Add(time.Minute)
	alert, err := domain.ReconstituteAlert(
		"alert-1", "robot-101", domain.AlertKindLowBattery,
		domain.AlertStatusClosed, openedAt, nil, &endedAt, domain.ClosureReasonRobotDeactivated,
	)
	require.NoError(t, err)

	response := serve(httpapi.Config{ListAlerts: &fakeListAlerts{alerts: []*domain.Alert{alert}}}, http.MethodGet, "/api/alerts?state=terminal", "")

	require.Equal(t, http.StatusOK, response.Code)
	var body struct {
		Alerts []map[string]any `json:"alerts"`
	}
	decodeBody(t, response.Body.Bytes(), &body)
	require.Len(t, body.Alerts, 1)
	assert.Equal(t, "2026-10-25T15:01:00Z", body.Alerts[0]["ended_at"])
	assert.Equal(t, "ROBOT_DEACTIVATED", body.Alerts[0]["closure_reason"])
}

func TestListAlertsPassesStateAndLimit(t *testing.T) {
	useCase := &fakeListAlerts{}

	response := serve(httpapi.Config{ListAlerts: useCase}, http.MethodGet, "/api/alerts?state=terminal&limit=10", "")

	require.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, application.ListAlertsQuery{Terminal: true, Limit: 10}, useCase.query)
	assert.JSONEq(t, `{"alerts": []}`, response.Body.String())
}

func TestListAlertsRejectsBadParameters(t *testing.T) {
	for _, target := range []string{
		"/api/alerts?state=open",
		"/api/alerts?limit=abc",
		"/api/alerts?limit=0",
		"/api/alerts?limit=-5",
	} {
		t.Run(target, func(t *testing.T) {
			useCase := &fakeListAlerts{}

			response := serve(httpapi.Config{ListAlerts: useCase}, http.MethodGet, target, "")

			assert.Equal(t, http.StatusBadRequest, response.Code)
			assert.Equal(t, "INVALID_REQUEST", errorCode(t, response))
			assert.False(t, useCase.called)
		})
	}
}

func TestListAlertsHidesInternalErrors(t *testing.T) {
	response := serve(httpapi.Config{ListAlerts: &fakeListAlerts{err: errors.New("connection reset")}}, http.MethodGet, "/api/alerts", "")

	assert.Equal(t, http.StatusInternalServerError, response.Code)
	assert.Equal(t, "INTERNAL", errorCode(t, response))
}

type fakeListAlerts struct {
	alerts []*domain.Alert
	err    error
	called bool
	query  application.ListAlertsQuery
}

func (useCase *fakeListAlerts) Execute(_ context.Context, query application.ListAlertsQuery) ([]*domain.Alert, error) {
	useCase.called = true
	useCase.query = query

	return useCase.alerts, useCase.err
}

func TestAcknowledgeAlertReturnsTheAcknowledgedAlert(t *testing.T) {
	openedAt := time.Date(2026, time.October, 25, 15, 0, 0, 0, time.UTC)
	acknowledgedAt := openedAt.Add(time.Minute)
	alert, err := domain.ReconstituteAlert(
		"alert-1", "robot-101", domain.AlertKindLowBattery,
		domain.AlertStatusAcknowledged, openedAt, &acknowledgedAt, nil, "",
	)
	require.NoError(t, err)
	useCase := &fakeAcknowledgeAlert{alert: alert}

	response := serve(httpapi.Config{AcknowledgeAlert: useCase}, http.MethodPost, "/api/alerts/alert-1/acknowledge", "")

	require.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, "alert-1", useCase.alertID)
	assert.JSONEq(t, `{
		"id": "alert-1",
		"robot_id": "robot-101",
		"kind": "LOW_BATTERY",
		"status": "ACKNOWLEDGED",
		"opened_at": "2026-10-25T15:00:00Z",
		"acknowledged_at": "2026-10-25T15:01:00Z",
		"ended_at": null,
		"closure_reason": null
	}`, response.Body.String())
}

func TestAcknowledgeAlertMapsErrors(t *testing.T) {
	tests := map[string]struct {
		err        error
		wantStatus int
		wantCode   string
	}{
		"unknown alert":    {application.ErrAlertNotFound, http.StatusNotFound, "ALERT_NOT_FOUND"},
		"alert has ended":  {domain.ErrAlertTerminal, http.StatusConflict, "ALERT_TERMINAL"},
		"unexpected fault": {errors.New("connection reset"), http.StatusInternalServerError, "INTERNAL"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			response := serve(httpapi.Config{AcknowledgeAlert: &fakeAcknowledgeAlert{err: tt.err}}, http.MethodPost, "/api/alerts/alert-1/acknowledge", "")

			assert.Equal(t, tt.wantStatus, response.Code)
			assert.Equal(t, tt.wantCode, errorCode(t, response))
			assert.NotContains(t, response.Body.String(), "connection reset")
		})
	}
}

type fakeAcknowledgeAlert struct {
	alert   *domain.Alert
	err     error
	alertID string
}

func (useCase *fakeAcknowledgeAlert) Execute(_ context.Context, alertID string) (*domain.Alert, error) {
	useCase.alertID = alertID

	return useCase.alert, useCase.err
}
