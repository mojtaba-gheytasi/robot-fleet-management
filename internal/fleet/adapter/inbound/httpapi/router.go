// Package httpapi translates HTTP requests into application commands and
// application results and errors into HTTP responses. It is the only place
// that knows HTTP status codes.
package httpapi

import (
	"log/slog"
	"net/http"
)

type Config struct {
	Logger        *slog.Logger
	RegisterRobot RegisterRobotUseCase
	GetRobot      GetRobotUseCase
	ListRobots    ListRobotsUseCase
}

func NewHandler(config Config) http.Handler {
	robots := &robotHandler{
		logger:        config.Logger,
		registerRobot: config.RegisterRobot,
		getRobot:      config.GetRobot,
		listRobots:    config.ListRobots,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/robots", robots.register)
	mux.HandleFunc("GET /api/robots", robots.list)
	mux.HandleFunc("GET /api/robots/{id}", robots.get)

	return mux
}
