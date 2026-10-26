package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/application"
)

type RecordTelemetryUseCase interface {
	Execute(ctx context.Context, command application.RecordTelemetryCommand) (application.RecordTelemetryResult, error)
}

type telemetryHandler struct {
	logger          *slog.Logger
	recordTelemetry RecordTelemetryUseCase
}

// Pointers, so a missing battery_percent is not read as 0%.
type recordTelemetryRequest struct {
	ObservationID  *string          `json:"observation_id"`
	ObservedAt     *time.Time       `json:"observed_at"`
	BatteryPercent *float64         `json:"battery_percent"`
	Position       *positionRequest `json:"position"`
	OperatingMode  *string          `json:"operating_mode"`
}

type positionRequest struct {
	X *float64 `json:"x"`
	Y *float64 `json:"y"`
}

type recordTelemetryResponse struct {
	Outcome       string `json:"outcome"`
	RobotID       string `json:"robot_id"`
	ObservationID string `json:"observation_id"`
}

func (handler *telemetryHandler) record(w http.ResponseWriter, r *http.Request) {
	var request recordTelemetryRequest
	if err := decodeJSON(w, r, &request); err != nil {
		respondError(w, http.StatusBadRequest, codeInvalidRequest, err.Error())
		return
	}

	if missing := request.missingFields(); len(missing) > 0 {
		respondError(
			w,
			http.StatusBadRequest,
			codeInvalidRequest,
			"missing required fields: "+strings.Join(missing, ", "),
		)
		return
	}

	command := application.RecordTelemetryCommand{
		RobotID:        r.PathValue("id"),
		ObservationID:  *request.ObservationID,
		ObservedAt:     *request.ObservedAt,
		BatteryPercent: *request.BatteryPercent,
		PositionX:      *request.Position.X,
		PositionY:      *request.Position.Y,
		OperatingMode:  *request.OperatingMode,
	}

	result, err := handler.recordTelemetry.Execute(r.Context(), command)
	if err != nil {
		respondUseCaseError(w, r, handler.logger, err)
		return
	}

	writeJSON(w, http.StatusOK, recordTelemetryResponse{
		Outcome:       string(result.Outcome),
		RobotID:       command.RobotID,
		ObservationID: command.ObservationID,
	})
}

func (request recordTelemetryRequest) missingFields() []string {
	missing := make([]string, 0)
	if request.ObservationID == nil {
		missing = append(missing, "observation_id")
	}
	if request.ObservedAt == nil {
		missing = append(missing, "observed_at")
	}
	if request.BatteryPercent == nil {
		missing = append(missing, "battery_percent")
	}
	if request.Position == nil || request.Position.X == nil {
		missing = append(missing, "position.x")
	}
	if request.Position == nil || request.Position.Y == nil {
		missing = append(missing, "position.y")
	}
	if request.OperatingMode == nil {
		missing = append(missing, "operating_mode")
	}

	return missing
}
