package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/application"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

type ListAlertsUseCase interface {
	Execute(ctx context.Context, query application.ListAlertsQuery) ([]*domain.Alert, error)
}

type alertHandler struct {
	logger     *slog.Logger
	listAlerts ListAlertsUseCase
}

type alertResponse struct {
	ID             string     `json:"id"`
	RobotID        string     `json:"robot_id"`
	Kind           string     `json:"kind"`
	Status         string     `json:"status"`
	OpenedAt       time.Time  `json:"opened_at"`
	AcknowledgedAt *time.Time `json:"acknowledged_at"`
	EndedAt        *time.Time `json:"ended_at"`
	ClosureReason  *string    `json:"closure_reason"`
}

type alertListResponse struct {
	Alerts []alertResponse `json:"alerts"`
}

func (handler *alertHandler) list(w http.ResponseWriter, r *http.Request) {
	query := application.ListAlertsQuery{}

	switch r.URL.Query().Get("state") {
	case "", "unresolved":
	case "terminal":
		query.Terminal = true
	default:
		respondError(w, http.StatusBadRequest, codeInvalidRequest, "state must be unresolved or terminal")
		return
	}

	if value := r.URL.Query().Get("limit"); value != "" {
		limit, err := strconv.Atoi(value)
		if err != nil || limit <= 0 {
			respondError(w, http.StatusBadRequest, codeInvalidRequest, "limit must be a positive integer")
			return
		}
		query.Limit = limit
	}

	alerts, err := handler.listAlerts.Execute(r.Context(), query)
	if err != nil {
		respondUseCaseError(w, r, handler.logger, err)
		return
	}

	body := alertListResponse{Alerts: make([]alertResponse, 0, len(alerts))}
	for _, alert := range alerts {
		body.Alerts = append(body.Alerts, alertResponseFrom(alert))
	}

	writeJSON(w, http.StatusOK, body)
}

func alertResponseFrom(alert *domain.Alert) alertResponse {
	response := alertResponse{
		ID:       string(alert.ID()),
		RobotID:  string(alert.RobotID()),
		Kind:     string(alert.Kind()),
		Status:   string(alert.Status()),
		OpenedAt: alert.OpenedAt().UTC(),
	}

	if acknowledgedAt, ok := alert.AcknowledgedAt(); ok {
		utc := acknowledgedAt.UTC()
		response.AcknowledgedAt = &utc
	}

	if endedAt, ok := alert.EndedAt(); ok {
		utc := endedAt.UTC()
		response.EndedAt = &utc
	}

	if reason := alert.ClosureReason(); reason != "" {
		value := string(reason)
		response.ClosureReason = &value
	}

	return response
}
