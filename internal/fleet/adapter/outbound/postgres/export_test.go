package postgres

// Exposed to the external test package so the EXPLAIN test checks the real
// queries, not copies of them.
var (
	FindRobotByIDQuery = findRobotByIDQuery
	ListRobotsQuery    = listRobotsQuery
)
