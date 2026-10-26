package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/application"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

type RegisterRobotUseCase interface {
	Execute(ctx context.Context, command application.RegisterRobotCommand) (application.RegisterRobotResult, error)
}

type GetRobotUseCase interface {
	Execute(ctx context.Context, robotID string) (application.RobotState, error)
}

type ListRobotsUseCase interface {
	Execute(ctx context.Context, query application.ListRobotsQuery) ([]application.RobotState, error)
}

type ChangeLifecycleUseCase interface {
	Execute(ctx context.Context, robotID string) (application.RobotState, error)
}

type robotHandler struct {
	logger          *slog.Logger
	registerRobot   RegisterRobotUseCase
	getRobot        GetRobotUseCase
	listRobots      ListRobotsUseCase
	deactivateRobot ChangeLifecycleUseCase
	activateRobot   ChangeLifecycleUseCase
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

type observationResponse struct {
	ObservationID  string           `json:"observation_id"`
	ObservedAt     time.Time        `json:"observed_at"`
	ReceivedAt     time.Time        `json:"received_at"`
	BatteryPercent float64          `json:"battery_percent"`
	Position       positionResponse `json:"position"`
	OperatingMode  string           `json:"operating_mode"`
}

type positionResponse struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type robotListResponse struct {
	Robots []robotResponse `json:"robots"`
}

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

func (handler *robotHandler) get(w http.ResponseWriter, r *http.Request) {
	state, err := handler.getRobot.Execute(r.Context(), r.PathValue("id"))
	if err != nil {
		respondUseCaseError(w, r, handler.logger, err)
		return
	}

	writeJSON(w, http.StatusOK, robotResponseFrom(state))
}

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

func (handler *robotHandler) deactivate(w http.ResponseWriter, r *http.Request) {
	handler.changeLifecycle(w, r, handler.deactivateRobot)
}

func (handler *robotHandler) activate(w http.ResponseWriter, r *http.Request) {
	handler.changeLifecycle(w, r, handler.activateRobot)
}

func (handler *robotHandler) changeLifecycle(w http.ResponseWriter, r *http.Request, useCase ChangeLifecycleUseCase) {
	state, err := useCase.Execute(r.Context(), r.PathValue("id"))
	if err != nil {
		respondUseCaseError(w, r, handler.logger, err)
		return
	}

	writeJSON(w, http.StatusOK, robotResponseFrom(state))
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
		LatestObservation: latestObservationResponseFrom(robot),
	}
}

func latestObservationResponseFrom(robot *domain.Robot) *observationResponse {
	latest, ok := robot.LatestObservation()
	if ok == false {
		return nil
	}

	observation := latest.Observation

	return &observationResponse{
		ObservationID:  string(observation.ID()),
		ObservedAt:     observation.ObservedAt().UTC(),
		ReceivedAt:     latest.ReceivedAt.UTC(),
		BatteryPercent: observation.Battery().Percent(),
		Position: positionResponse{
			X: observation.Position().X(),
			Y: observation.Position().Y(),
		},
		OperatingMode: string(observation.OperatingMode()),
	}
}
