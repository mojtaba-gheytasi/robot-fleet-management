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

	RecordTelemetry RecordTelemetryUseCase
}

func NewHandler(config Config) http.Handler {
	robots := &robotHandler{
		logger:        config.Logger,
		registerRobot: config.RegisterRobot,
		getRobot:      config.GetRobot,
		listRobots:    config.ListRobots,
	}

	telemetry := &telemetryHandler{
		logger:          config.Logger,
		recordTelemetry: config.RecordTelemetry,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/robots", robots.register)
	mux.HandleFunc("GET /api/robots", robots.list)
	mux.HandleFunc("GET /api/robots/{id}", robots.get)
	mux.HandleFunc("POST /api/robots/{id}/telemetry", telemetry.record)

	return mux
}
