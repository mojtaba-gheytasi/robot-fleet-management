package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/application"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

const (
	codeInvalidRequest            = "INVALID_REQUEST"
	codeRobotNotFound             = "ROBOT_NOT_FOUND"
	codeSiteNotFound              = "SITE_NOT_FOUND"
	codeRobotRegistrationConflict = "ROBOT_REGISTRATION_CONFLICT"
	codeInvalidObservation        = "INVALID_OBSERVATION"
	codeObservationConflict       = "OBSERVATION_CONFLICT"
	codeClockSkewExceeded         = "CLOCK_SKEW_EXCEEDED"
	codeAlertNotFound             = "ALERT_NOT_FOUND"
	codeAlertTerminal             = "ALERT_TERMINAL"
	codeInternal                  = "INTERNAL"
)

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorResponse struct {
	Error errorBody `json:"error"`
}

func respondError(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, errorResponse{Error: errorBody{Code: code, Message: message}})
}

func respondUseCaseError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidRobot):
		respondError(w, http.StatusBadRequest, codeInvalidRequest, err.Error())
	case errors.Is(err, domain.ErrInvalidObservation):
		respondError(w, http.StatusUnprocessableEntity, codeInvalidObservation, err.Error())
	case errors.Is(err, domain.ErrClockSkewExceeded):
		respondError(w, http.StatusUnprocessableEntity, codeClockSkewExceeded, err.Error())
	case errors.Is(err, domain.ErrObservationConflict):
		respondError(
			w,
			http.StatusConflict,
			codeObservationConflict,
			"this observation id was already used with different content",
		)
	case errors.Is(err, domain.ErrAlertTerminal):
		respondError(w, http.StatusConflict, codeAlertTerminal, "the alert is resolved or closed and cannot be acknowledged")
	case errors.Is(err, application.ErrAlertNotFound):
		respondError(w, http.StatusNotFound, codeAlertNotFound, "the alert does not exist")
	case errors.Is(err, application.ErrRobotNotFound):
		respondError(w, http.StatusNotFound, codeRobotNotFound, "the robot does not exist")
	case errors.Is(err, application.ErrSiteNotFound):
		respondError(w, http.StatusNotFound, codeSiteNotFound, "the site does not exist")
	case errors.Is(err, application.ErrRobotRegistrationConflict):
		respondError(
			w,
			http.StatusConflict,
			codeRobotRegistrationConflict,
			"a robot with this id is already registered with different data",
		)
	default:
		logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "error", err)
		respondError(w, http.StatusInternalServerError, codeInternal, "an unexpected error occurred")
	}
}
