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
	insertRobotQuery = `
		INSERT INTO robots (id, site_id, lifecycle, created_at, activated_at)
		VALUES ($1, $2, $3, $4, $4)
		ON CONFLICT (id) DO NOTHING`

	// Shaped so the partial index answers the lateral lookup; see the EXPLAIN test.
	selectRobotsQuery = `
		SELECT
			r.id, r.site_id, r.lifecycle, r.created_at, r.activated_at, r.battery_health,
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

	lockRobotQuery = `
		SELECT id FROM robots WHERE id = $1 FOR UPDATE`

	updateRobotQuery = `
		UPDATE robots SET lifecycle = $2, activated_at = $3, battery_health = $4 WHERE id = $1`

	closeUnresolvedAlertsQuery = `
		UPDATE alerts SET status = 'CLOSED', closure_reason = $2, ended_at = $3
		WHERE robot_id = $1 AND status IN ('OPEN', 'ACKNOWLEDGED')`

	findObservationQuery = `
		SELECT observation_id, observed_at, battery_percent, position_x, position_y, operating_mode
		FROM telemetry_observations
		WHERE robot_id = $1 AND observation_id = $2`

	insertObservationQuery = `
		INSERT INTO telemetry_observations (
			robot_id, observation_id, observed_at, received_at,
			battery_percent, position_x, position_y, operating_mode, outcome
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

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

func (repository *RobotRepository) WithLockedRobot(
	ctx context.Context,
	id domain.RobotID,
	fn func(robot *domain.Robot, tx application.RobotTx) error,
) error {
	tx, err := repository.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var lockedID string
	err = tx.QueryRowContext(ctx, lockRobotQuery, string(id)).Scan(&lockedID)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ErrRobotNotFound
	}
	if err != nil {
		return fmt.Errorf("lock robot: %w", err)
	}

	// A separate statement after the lock, so it sees what the previous holder committed.
	robot, err := scanRobot(tx.QueryRowContext(ctx, findRobotByIDQuery, string(id)))
	if err != nil {
		return fmt.Errorf("load locked robot: %w", err)
	}

	loaded := savedRobotState(robot)

	if err := fn(robot, &robotTx{tx: tx, robotID: id}); err != nil {
		return err
	}

	if changed := savedRobotState(robot); changed != loaded {
		_, err := tx.ExecContext(ctx, updateRobotQuery,
			string(id), string(changed.lifecycle), changed.activatedAt, string(changed.batteryHealth))
		if err != nil {
			return fmt.Errorf("update robot: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

type robotState struct {
	lifecycle     domain.Lifecycle
	activatedAt   time.Time
	batteryHealth domain.BatteryHealth
}

func savedRobotState(robot *domain.Robot) robotState {
	return robotState{
		lifecycle:     robot.Lifecycle(),
		activatedAt:   robot.ActivatedAt(),
		batteryHealth: robot.BatteryHealth(),
	}
}

type robotTx struct {
	tx      *sql.Tx
	robotID domain.RobotID
}

func (log *robotTx) FindObservation(ctx context.Context, id domain.ObservationID) (domain.Observation, bool, error) {
	var (
		observationID  string
		observedAt     time.Time
		batteryPercent float64
		positionX      float64
		positionY      float64
		operatingMode  string
	)

	err := log.tx.QueryRowContext(ctx, findObservationQuery, string(log.robotID), string(id)).
		Scan(&observationID, &observedAt, &batteryPercent, &positionX, &positionY, &operatingMode)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Observation{}, false, nil
	}
	if err != nil {
		return domain.Observation{}, false, fmt.Errorf("find observation: %w", err)
	}

	observation, err := observationFromRow(observationID, observedAt, batteryPercent, positionX, positionY, operatingMode)
	if err != nil {
		return domain.Observation{}, false, fmt.Errorf("corrupt stored observation %s: %w", observationID, err)
	}

	return observation, true, nil
}

func (log *robotTx) AddObservation(
	ctx context.Context,
	observation domain.Observation,
	receivedAt time.Time,
	outcome domain.ObservationOutcome,
) error {
	_, err := log.tx.ExecContext(
		ctx,
		insertObservationQuery,
		string(log.robotID),
		string(observation.ID()),
		observation.ObservedAt(),
		receivedAt,
		// As text, so numeric(5,2) stores the exact two decimals.
		observation.Battery().String(),
		observation.Position().X(),
		observation.Position().Y(),
		string(observation.OperatingMode()),
		string(outcome),
	)
	if err != nil {
		return fmt.Errorf("insert observation: %w", err)
	}

	return nil
}

func (log *robotTx) CloseUnresolvedAlerts(ctx context.Context, reason domain.ClosureReason, now time.Time) (int, error) {
	result, err := log.tx.ExecContext(ctx, closeUnresolvedAlertsQuery, string(log.robotID), string(reason), now)
	if err != nil {
		return 0, fmt.Errorf("close unresolved alerts: %w", err)
	}

	closed, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read closed alert count: %w", err)
	}

	return int(closed), nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRobot(row rowScanner) (*domain.Robot, error) {
	var (
		robotID       string
		siteID        string
		lifecycle     string
		registeredAt  time.Time
		activatedAt   time.Time
		batteryHealth string

		observationID  sql.NullString
		observedAt     sql.NullTime
		receivedAt     sql.NullTime
		batteryPercent sql.NullFloat64
		positionX      sql.NullFloat64
		positionY      sql.NullFloat64
		operatingMode  sql.NullString
	)

	if err := row.Scan(
		&robotID, &siteID, &lifecycle, &registeredAt, &activatedAt, &batteryHealth,
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
		activatedAt,
		domain.BatteryHealth(batteryHealth),
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
