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

	DeactivateRobot ChangeLifecycleUseCase
	ActivateRobot   ChangeLifecycleUseCase

	RecordTelemetry       RecordTelemetryUseCase
	GetObservationHistory GetObservationHistoryUseCase
	GetFleetSummary       GetFleetSummaryUseCase

	ListAlerts       ListAlertsUseCase
	AcknowledgeAlert AcknowledgeAlertUseCase
}

func NewHandler(config Config) http.Handler {
	robots := &robotHandler{
		logger:        config.Logger,
		registerRobot: config.RegisterRobot,
		getRobot:      config.GetRobot,
		listRobots:    config.ListRobots,

		deactivateRobot: config.DeactivateRobot,
		activateRobot:   config.ActivateRobot,
	}

	telemetry := &telemetryHandler{
		logger:          config.Logger,
		recordTelemetry: config.RecordTelemetry,
		history:         config.GetObservationHistory,
	}

	fleet := &fleetHandler{
		logger:  config.Logger,
		summary: config.GetFleetSummary,
	}

	alerts := &alertHandler{
		logger:           config.Logger,
		listAlerts:       config.ListAlerts,
		acknowledgeAlert: config.AcknowledgeAlert,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/robots", robots.register)
	mux.HandleFunc("GET /api/robots", robots.list)
	mux.HandleFunc("GET /api/robots/{id}", robots.get)
	mux.HandleFunc("POST /api/robots/{id}/deactivate", robots.deactivate)
	mux.HandleFunc("POST /api/robots/{id}/activate", robots.activate)
	mux.HandleFunc("POST /api/robots/{id}/telemetry", telemetry.record)
	mux.HandleFunc("GET /api/robots/{id}/telemetry", telemetry.list)
	mux.HandleFunc("GET /api/fleet/summary", fleet.getSummary)
	mux.HandleFunc("GET /api/alerts", alerts.list)
	mux.HandleFunc("POST /api/alerts/{id}/acknowledge", alerts.acknowledge)

	return mux
}
