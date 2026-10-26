package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/application"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

// RegisterRobotUseCase is declared here, where it is used, so handler tests
// can pass a fake.
type RegisterRobotUseCase interface {
	Execute(ctx context.Context, command application.RegisterRobotCommand) (application.RegisterRobotResult, error)
}

type robotHandler struct {
	logger        *slog.Logger
	registerRobot RegisterRobotUseCase
}

type registerRobotRequest struct {
	ID     string `json:"id"`
	SiteID string `json:"site_id"`
}

type robotResponse struct {
	ID           string    `json:"id"`
	SiteID       string    `json:"site_id"`
	Lifecycle    string    `json:"lifecycle"`
	RegisteredAt time.Time `json:"registered_at"`
}

// register handles POST /api/robots: 201 for a new robot, 200 for an
// identical retry.
func (handler *robotHandler) register(w http.ResponseWriter, r *http.Request) {
	var request registerRobotRequest
	if err := decodeJSON(w, r, &request); err != nil {
		respondError(w, http.StatusBadRequest, codeInvalidRequest, err.Error())
		return
	}

	result, err := handler.registerRobot.Execute(r.Context(), application.RegisterRobotCommand{
		RobotID: request.ID,
		SiteID:  request.SiteID,
	})
	if err != nil {
		respondUseCaseError(w, r, handler.logger, err)
		return
	}

	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}

	writeJSON(w, status, robotResponseFrom(result.Robot))
}

func robotResponseFrom(robot *domain.Robot) robotResponse {
	return robotResponse{
		ID:           string(robot.ID()),
		SiteID:       string(robot.SiteID()),
		Lifecycle:    string(robot.Lifecycle()),
		RegisteredAt: robot.RegisteredAt().UTC(),
	}
}
