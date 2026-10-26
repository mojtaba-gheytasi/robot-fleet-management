package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

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
		LIMIT $1`

	listTerminalAlertsQuery = selectAlertsQuery + `
		WHERE status IN ('RESOLVED', 'CLOSED')
		ORDER BY ended_at DESC, id
		LIMIT $1`
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
		return nil, fmt.Errorf("corrupt stored alert %s: %w", id, err)
	}

	return alert, nil
}

func nullableTime(value sql.NullTime) *time.Time {
	if value.Valid == false {
		return nil
	}

	return &value.Time
}
