// Package wiring is the fleet module's composition root: the only place that
// knows which concrete adapters sit behind the application's ports.
package wiring

import (
	"database/sql"
	"log/slog"
	"net/http"
	"time"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/adapter/inbound/httpapi"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/adapter/outbound/postgres"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/application"
)

type Dependencies struct {
	DB     *sql.DB
	Logger *slog.Logger
	Clock  func() time.Time
}

type Module struct {
	HTTPHandler http.Handler
}

func New(dependencies Dependencies) *Module {
	// PostgreSQL stores microseconds. Truncating here means a robot returned
	// right after it is created matches the one read back later.
	clock := application.Clock(func() time.Time {
		return dependencies.Clock().Truncate(time.Microsecond)
	})

	robots := postgres.NewRobotRepository(dependencies.DB)

	return &Module{
		HTTPHandler: httpapi.NewHandler(httpapi.Config{
			Logger:        dependencies.Logger,
			RegisterRobot: application.NewRegisterRobot(robots, clock),
		}),
	}
}
