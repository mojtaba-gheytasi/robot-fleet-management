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
	selectAlertsQuery = `
		SELECT id::text, robot_id, kind, status, opened_at, acknowledged_at, ended_at, closure_reason
		FROM alerts`

	listUnresolvedAlertsQuery = selectAlertsQuery + `
		WHERE status IN ('OPEN', 'ACKNOWLEDGED')
		ORDER BY opened_at DESC, id
		LIMIT NULLIF($1, 0)`

	listTerminalAlertsQuery = selectAlertsQuery + `
		WHERE status IN ('RESOLVED', 'CLOSED')
		ORDER BY ended_at DESC, id
		LIMIT NULLIF($1, 0)`

	lockAlertQuery = selectAlertsQuery + `
		WHERE id = $1
		FOR UPDATE`

	// Answered by alerts_one_unresolved_idx; see the EXPLAIN test.
	lockUnresolvedAlertQuery = selectAlertsQuery + `
		WHERE robot_id = $1 AND kind = 'LOW_BATTERY' AND status IN ('OPEN', 'ACKNOWLEDGED')
		FOR UPDATE`

	insertAlertQuery = `
		INSERT INTO alerts (id, robot_id, kind, status, opened_at)
		VALUES ($1, $2, $3, $4, $5)`

	updateAlertQuery = `
		UPDATE alerts SET status = $2, acknowledged_at = $3, ended_at = $4, closure_reason = $5
		WHERE id = $1`

	invalidTextRepresentation = "22P02"
)

type AlertRepository struct {
	db *sql.DB
}

var _ application.AlertRepository = (*AlertRepository)(nil)

func NewAlertRepository(db *sql.DB) *AlertRepository {
	return &AlertRepository{db: db}
}

func (repository *AlertRepository) List(ctx context.Context, terminal bool, limit int) ([]*domain.Alert, error) {
	query := listUnresolvedAlertsQuery
	if terminal {
		query = listTerminalAlertsQuery
	}

	rows, err := repository.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("list alerts: %w", err)
	}
	defer rows.Close()

	alerts := make([]*domain.Alert, 0)
	for rows.Next() {
		alert, err := scanAlert(rows)
		if err != nil {
			return nil, err
		}

		alerts = append(alerts, alert)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate alerts: %w", err)
	}

	return alerts, nil
}

func (repository *AlertRepository) WithLockedAlert(
	ctx context.Context,
	id domain.AlertID,
	fn func(alert *domain.Alert) error,
) error {
	tx, err := repository.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	alert, err := scanAlert(tx.QueryRowContext(ctx, lockAlertQuery, string(id)))
	if errors.Is(err, sql.ErrNoRows) || isNotAUUID(err) {
		return application.ErrAlertNotFound
	}
	if err != nil {
		return fmt.Errorf("lock alert: %w", err)
	}

	loadedStatus := alert.Status()

	if err := fn(alert); err != nil {
		return err
	}

	if alert.Status() != loadedStatus {
		if err := updateAlert(ctx, tx, alert); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

func insertAlert(ctx context.Context, tx *sql.Tx, alert *domain.Alert) error {
	_, err := tx.ExecContext(ctx, insertAlertQuery,
		string(alert.ID()), string(alert.RobotID()), string(alert.Kind()), string(alert.Status()), alert.OpenedAt())
	if err != nil {
		return fmt.Errorf("insert alert: %w", err)
	}

	return nil
}

func updateAlert(ctx context.Context, tx *sql.Tx, alert *domain.Alert) error {
	acknowledgedAt, _ := alert.AcknowledgedAt()
	endedAt, _ := alert.EndedAt()
	closureReason := sql.NullString{String: string(alert.ClosureReason()), Valid: alert.ClosureReason() != ""}

	_, err := tx.ExecContext(ctx, updateAlertQuery,
		string(alert.ID()), string(alert.Status()), nullTime(acknowledgedAt), nullTime(endedAt), closureReason)
	if err != nil {
		return fmt.Errorf("update alert: %w", err)
	}

	return nil
}

func scanAlert(row rowScanner) (*domain.Alert, error) {
	var (
		id             string
		robotID        string
		kind           string
		status         string
		openedAt       time.Time
		acknowledgedAt sql.NullTime
		endedAt        sql.NullTime
		closureReason  sql.NullString
	)

	if err := row.Scan(&id, &robotID, &kind, &status, &openedAt, &acknowledgedAt, &endedAt, &closureReason); err != nil {
		return nil, fmt.Errorf("scan alert: %w", err)
	}

	alert, err := domain.ReconstituteAlert(
		domain.AlertID(id),
		domain.RobotID(robotID),
		domain.AlertKind(kind),
		domain.AlertStatus(status),
		openedAt,
		nullableTime(acknowledgedAt),
		nullableTime(endedAt),
		domain.ClosureReason(closureReason.String),
	)
	if err != nil {
		return nil, corruptData("alert "+id, err)
	}

	return alert, nil
}

func nullTime(value time.Time) sql.NullTime {
	return sql.NullTime{Time: value, Valid: value.IsZero() == false}
}

func isNotAUUID(err error) bool {
	var postgresError *pgconn.PgError

	return errors.As(err, &postgresError) && postgresError.Code == invalidTextRepresentation
}

func nullableTime(value sql.NullTime) *time.Time {
	if value.Valid == false {
		return nil
	}

	return &value.Time
}
