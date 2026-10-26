package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/application"
)

// The use case interfaces are declared here, where they are used, so handler
// tests can pass fakes.
type RegisterRobotUseCase interface {
	Execute(ctx context.Context, command application.RegisterRobotCommand) (application.RegisterRobotResult, error)
}

type GetRobotUseCase interface {
	Execute(ctx context.Context, robotID string) (application.RobotState, error)
}

type ListRobotsUseCase interface {
	Execute(ctx context.Context, query application.ListRobotsQuery) ([]application.RobotState, error)
}

type robotHandler struct {
	logger        *slog.Logger
	registerRobot RegisterRobotUseCase
	getRobot      GetRobotUseCase
	listRobots    ListRobotsUseCase
}

type registerRobotRequest struct {
	ID     string `json:"id"`
	SiteID string `json:"site_id"`
}

type robotResponse struct {
	ID                string               `json:"id"`
	SiteID            string               `json:"site_id"`
	Lifecycle         string               `json:"lifecycle"`
	RegisteredAt      time.Time            `json:"registered_at"`
	BatteryHealth     string               `json:"battery_health"`
	Connectivity      string               `json:"connectivity"`
	LatestObservation *observationResponse `json:"latest_observation"`
}

// observationResponse is the robot's latest applied observation. It is null
// until the robot reports one, rather than filled with made-up values (VIS-06).
type observationResponse struct{}

type robotListResponse struct {
	Robots []robotResponse `json:"robots"`
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

	writeJSON(w, status, robotResponseFrom(result.RobotState))
}

// get handles GET /api/robots/{id}.
func (handler *robotHandler) get(w http.ResponseWriter, r *http.Request) {
	state, err := handler.getRobot.Execute(r.Context(), r.PathValue("id"))
	if err != nil {
		respondUseCaseError(w, r, handler.logger, err)
		return
	}

	writeJSON(w, http.StatusOK, robotResponseFrom(state))
}

// list handles GET /api/robots with an optional site_id filter.
func (handler *robotHandler) list(w http.ResponseWriter, r *http.Request) {
	states, err := handler.listRobots.Execute(r.Context(), application.ListRobotsQuery{
		SiteID: r.URL.Query().Get("site_id"),
	})
	if err != nil {
		respondUseCaseError(w, r, handler.logger, err)
		return
	}

	robots := make([]robotResponse, 0, len(states))
	for _, state := range states {
		robots = append(robots, robotResponseFrom(state))
	}

	writeJSON(w, http.StatusOK, robotListResponse{Robots: robots})
}

func robotResponseFrom(state application.RobotState) robotResponse {
	robot := state.Robot

	return robotResponse{
		ID:                string(robot.ID()),
		SiteID:            string(robot.SiteID()),
		Lifecycle:         string(robot.Lifecycle()),
		RegisteredAt:      robot.RegisteredAt().UTC(),
		BatteryHealth:     string(robot.BatteryHealth()),
		Connectivity:      string(state.Connectivity),
		LatestObservation: nil,
	}
}
