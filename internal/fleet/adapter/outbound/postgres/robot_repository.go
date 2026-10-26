package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/application"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/fleet/domain"
)

const (
	// ON CONFLICT DO NOTHING leaves an existing robot untouched, so two
	// identical registrations racing each other both end without an error
	// and the use case decides what the second one means.
	insertRobotQuery = `
		INSERT INTO robots (id, site_id, lifecycle, created_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (id) DO NOTHING`

	// selectRobotsQuery loads robots with their current observation. The
	// lateral subquery asks "newest APPLIED observation of this robot" in
	// exactly the shape the partial index answers, so it is one index lookup
	// per robot whatever the size of the history.
	selectRobotsQuery = `
		SELECT
			r.id, r.site_id, r.lifecycle, r.created_at,
			o.observation_id, o.observed_at, o.received_at,
			o.battery_percent, o.position_x, o.position_y, o.operating_mode
		FROM robots r
		LEFT JOIN LATERAL (
			SELECT observation_id, observed_at, received_at,
				battery_percent, position_x, position_y, operating_mode
			FROM telemetry_observations
			WHERE robot_id = r.id AND outcome = 'APPLIED'
			ORDER BY observed_at DESC
			LIMIT 1
		) o ON true`

	findRobotByIDQuery = selectRobotsQuery + `
		WHERE r.id = $1`

	listRobotsQuery = selectRobotsQuery + `
		WHERE $1::text IS NULL OR r.site_id = $1
		ORDER BY r.id`

	insertAppliedObservationQuery = `
		INSERT INTO telemetry_observations (
			robot_id, observation_id, observed_at, received_at,
			battery_percent, position_x, position_y, operating_mode, outcome
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'APPLIED')`

	foreignKeyViolation = "23503"
	siteForeignKey      = "robots_site_id_fkey"
)

type RobotRepository struct {
	db *sql.DB
}

var _ application.RobotRepository = (*RobotRepository)(nil)

func NewRobotRepository(db *sql.DB) *RobotRepository {
	return &RobotRepository{db: db}
}

func (repository *RobotRepository) Create(ctx context.Context, robot *domain.Robot) error {
	result, err := repository.db.ExecContext(
		ctx,
		insertRobotQuery,
		string(robot.ID()),
		string(robot.SiteID()),
		string(robot.Lifecycle()),
		robot.RegisteredAt(),
	)
	if err != nil {
		if isUnknownSite(err) {
			return application.ErrSiteNotFound
		}

		return fmt.Errorf("insert robot: %w", err)
	}

	inserted, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read inserted robot count: %w", err)
	}
	if inserted == 0 {
		return application.ErrRobotAlreadyExists
	}

	return nil
}

func (repository *RobotRepository) FindByID(ctx context.Context, id domain.RobotID) (*domain.Robot, error) {
	robot, err := scanRobot(repository.db.QueryRowContext(ctx, findRobotByIDQuery, string(id)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, application.ErrRobotNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find robot: %w", err)
	}

	return robot, nil
}

func (repository *RobotRepository) List(ctx context.Context, siteID *domain.SiteID) ([]*domain.Robot, error) {
	// A nil filter is sent as SQL NULL, which the query reads as "every site".
	var siteFilter any
	if siteID != nil {
		siteFilter = string(*siteID)
	}

	rows, err := repository.db.QueryContext(ctx, listRobotsQuery, siteFilter)
	if err != nil {
		return nil, fmt.Errorf("list robots: %w", err)
	}
	defer rows.Close()

	robots := make([]*domain.Robot, 0)
	for rows.Next() {
		robot, err := scanRobot(rows)
		if err != nil {
			return nil, fmt.Errorf("list robots: %w", err)
		}

		robots = append(robots, robot)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate robots: %w", err)
	}

	return robots, nil
}

func (repository *RobotRepository) AddAppliedObservation(
	ctx context.Context,
	robotID domain.RobotID,
	applied domain.AppliedObservation,
) error {
	observation := applied.Observation

	_, err := repository.db.ExecContext(
		ctx,
		insertAppliedObservationQuery,
		string(robotID),
		string(observation.ID()),
		observation.ObservedAt(),
		applied.ReceivedAt,
		// Sent as text so numeric(5,2) stores exactly the two decimals.
		observation.Battery().String(),
		observation.Position().X(),
		observation.Position().Y(),
		string(observation.OperatingMode()),
	)
	if err != nil {
		return fmt.Errorf("insert applied observation: %w", err)
	}

	return nil
}

// rowScanner is the part of *sql.Row and *sql.Rows that scanRobot needs.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanRobot(row rowScanner) (*domain.Robot, error) {
	var (
		robotID      string
		siteID       string
		lifecycle    string
		registeredAt time.Time

		// NULL for every observation column when the robot has not reported.
		observationID  sql.NullString
		observedAt     sql.NullTime
		receivedAt     sql.NullTime
		batteryPercent sql.NullFloat64
		positionX      sql.NullFloat64
		positionY      sql.NullFloat64
		operatingMode  sql.NullString
	)

	if err := row.Scan(
		&robotID, &siteID, &lifecycle, &registeredAt,
		&observationID, &observedAt, &receivedAt,
		&batteryPercent, &positionX, &positionY, &operatingMode,
	); err != nil {
		return nil, err
	}

	var latest *domain.AppliedObservation
	if observationID.Valid {
		observation, err := observationFromRow(
			observationID.String,
			observedAt.Time,
			batteryPercent.Float64,
			positionX.Float64,
			positionY.Float64,
			operatingMode.String,
		)
		if err != nil {
			return nil, fmt.Errorf("corrupt stored observation of robot %s: %w", robotID, err)
		}

		latest = &domain.AppliedObservation{Observation: observation, ReceivedAt: receivedAt.Time}
	}

	robot, err := domain.ReconstituteRobot(
		domain.RobotID(robotID),
		domain.SiteID(siteID),
		domain.Lifecycle(lifecycle),
		registeredAt,
		latest,
	)
	if err != nil {
		return nil, fmt.Errorf("corrupt stored robot %s: %w", robotID, err)
	}

	return robot, nil
}

func observationFromRow(
	observationID string,
	observedAt time.Time,
	batteryPercent float64,
	positionX float64,
	positionY float64,
	operatingMode string,
) (domain.Observation, error) {
	battery, err := domain.NewBatteryLevel(batteryPercent)
	if err != nil {
		return domain.Observation{}, err
	}

	position, err := domain.NewPosition(positionX, positionY)
	if err != nil {
		return domain.Observation{}, err
	}

	mode, err := domain.NewOperatingMode(operatingMode)
	if err != nil {
		return domain.Observation{}, err
	}

	return domain.NewObservation(domain.ObservationID(observationID), observedAt, battery, position, mode)
}

func isUnknownSite(err error) bool {
	var postgresError *pgconn.PgError

	return errors.As(err, &postgresError) &&
		postgresError.Code == foreignKeyViolation &&
		postgresError.ConstraintName == siteForeignKey
}
