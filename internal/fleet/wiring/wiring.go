package wiring

import (
	"database/sql"
	"log/slog"
	"net/http"
	"time"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/adapter/inbound/alertcheck"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/adapter/inbound/httpapi"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/adapter/outbound/postgres"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/application"
)

type Dependencies struct {
	DB                 *sql.DB
	Logger             *slog.Logger
	Clock              func() time.Time
	AlertCheckInterval time.Duration
}

type Module struct {
	HTTPHandler http.Handler

	// AlertCheck must be stopped before the database is closed.
	AlertCheck *alertcheck.AlertCheck
}

func New(dependencies Dependencies) *Module {
	// PostgreSQL stores microseconds.
	clock := application.Clock(func() time.Time {
		return dependencies.Clock().Truncate(time.Microsecond)
	})

	robots := postgres.NewRobotRepository(dependencies.DB)
	alerts := postgres.NewAlertRepository(dependencies.DB)
	reads := postgres.NewReadRepository(dependencies.DB)

	return &Module{
		HTTPHandler: httpapi.NewHandler(httpapi.Config{
			Logger:        dependencies.Logger,
			RegisterRobot: application.NewRegisterRobot(robots, clock),
			GetRobot:      application.NewGetRobot(robots, clock),
			ListRobots:    application.NewListRobots(robots, clock),

			DeactivateRobot: application.NewDeactivateRobot(robots, clock),
			ActivateRobot:   application.NewActivateRobot(robots, clock),

			RecordTelemetry:       application.NewRecordTelemetry(robots, clock),
			GetObservationHistory: application.NewGetObservationHistory(reads),
			GetFleetSummary:       application.NewGetFleetSummary(reads, clock),

			ListAlerts:       application.NewListAlerts(alerts),
			AcknowledgeAlert: application.NewAcknowledgeAlert(alerts, clock),
		}),
		AlertCheck: alertcheck.New(
			application.NewReconcileAlerts(postgres.NewAlertReconciler(dependencies.DB), clock),
			dependencies.AlertCheckInterval,
			dependencies.Logger,
		),
	}
}
