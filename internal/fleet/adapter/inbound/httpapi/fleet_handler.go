package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/application"
)

type GetFleetSummaryUseCase interface {
	Execute(ctx context.Context, query application.GetFleetSummaryQuery) (application.FleetSummary, error)
}

type fleetHandler struct {
	logger  *slog.Logger
	summary GetFleetSummaryUseCase
}

type fleetSummaryResponse struct {
	SiteID           *string `json:"site_id"`
	Registered       int     `json:"registered"`
	Active           int     `json:"active"`
	Inactive         int     `json:"inactive"`
	Online           int     `json:"online"`
	Stale            int     `json:"stale"`
	LowBattery       int     `json:"low_battery"`
	UnresolvedAlerts int     `json:"unresolved_alerts"`
}

func (handler *fleetHandler) getSummary(w http.ResponseWriter, r *http.Request) {
	siteID := r.URL.Query().Get("site_id")

	summary, err := handler.summary.Execute(r.Context(), application.GetFleetSummaryQuery{SiteID: siteID})
	if err != nil {
		respondUseCaseError(w, r, handler.logger, err)
		return
	}

	response := fleetSummaryResponse{
		Registered:       summary.Registered,
		Active:           summary.Active,
		Inactive:         summary.Inactive,
		Online:           summary.Online,
		Stale:            summary.Stale,
		LowBattery:       summary.LowBattery,
		UnresolvedAlerts: summary.UnresolvedAlerts,
	}
	if siteID != "" {
		response.SiteID = &siteID
	}

	writeJSON(w, http.StatusOK, response)
}
