package postgres

import "database/sql"

var (
	FindRobotByIDQuery = findRobotByIDQuery
	ListRobotsQuery    = listRobotsQuery

	LockUnresolvedAlertQuery = lockUnresolvedAlertQuery
	FleetSummaryQuery        = fleetSummaryQuery
	ObservationHistoryQuery  = observationHistoryQuery
)

func (repository *RobotRepository) DB() *sql.DB {
	return repository.db
}
