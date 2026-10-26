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

	findRobotByIDQuery = `
		SELECT id, site_id, lifecycle, created_at
		FROM robots
		WHERE id = $1`

	listRobotsQuery = `
		SELECT id, site_id, lifecycle, created_at
		FROM robots
		WHERE $1::text IS NULL OR site_id = $1
		ORDER BY id`

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
	)

	if err := row.Scan(&robotID, &siteID, &lifecycle, &registeredAt); err != nil {
		return nil, err
	}

	robot, err := domain.ReconstituteRobot(
		domain.RobotID(robotID),
		domain.SiteID(siteID),
		domain.Lifecycle(lifecycle),
		registeredAt,
	)
	if err != nil {
		return nil, fmt.Errorf("corrupt stored robot %s: %w", robotID, err)
	}

	return robot, nil
}

func isUnknownSite(err error) bool {
	var postgresError *pgconn.PgError

	return errors.As(err, &postgresError) &&
		postgresError.Code == foreignKeyViolation &&
		postgresError.ConstraintName == siteForeignKey
}
