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

// respondUseCaseError maps application and domain errors to a status and a
// stable code. Anything unexpected is logged and answered as INTERNAL,
// without exposing the cause.
func respondUseCaseError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidRobot):
		respondError(w, http.StatusBadRequest, codeInvalidRequest, err.Error())
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
