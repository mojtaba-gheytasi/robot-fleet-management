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
	fleetSummaryQuery = `
		SELECT
			count(*),
			count(*) FILTER (WHERE r.lifecycle = 'ACTIVE'),
			count(*) FILTER (WHERE r.lifecycle = 'INACTIVE'),
			count(*) FILTER (WHERE r.lifecycle = 'ACTIVE' AND o.received_at >= $2),
			count(*) FILTER (WHERE r.lifecycle = 'ACTIVE' AND o.received_at < $2),
			count(*) FILTER (WHERE r.lifecycle = 'ACTIVE' AND r.battery_health = 'LOW'),
			(
				SELECT count(*)
				FROM alerts a
				JOIN robots ar ON ar.id = a.robot_id
				WHERE a.status IN ('OPEN', 'ACKNOWLEDGED')
					AND ($1::text IS NULL OR ar.site_id = $1)
			)
		FROM robots r
		LEFT JOIN LATERAL (
			SELECT received_at
			FROM telemetry_observations
			WHERE robot_id = r.id AND outcome = 'APPLIED'
			ORDER BY observed_at DESC
			LIMIT 1
		) o ON true
		WHERE $1::text IS NULL OR r.site_id = $1`

	robotExistsQuery = `SELECT EXISTS (SELECT 1 FROM robots WHERE id = $1)`

	observationHistoryQuery = `
		SELECT observation_id, observed_at, received_at, battery_percent,
			position_x, position_y, operating_mode, outcome
		FROM telemetry_observations
		WHERE robot_id = $1
		ORDER BY observed_at DESC, observation_id
		LIMIT $2`
)

type ReadRepository struct {
	db *sql.DB
}

var _ application.FleetReadRepository = (*ReadRepository)(nil)

func NewReadRepository(db *sql.DB) *ReadRepository {
	return &ReadRepository{db: db}
}

func (repository *ReadRepository) Summary(
	ctx context.Context,
	siteID *domain.SiteID,
	onlineSince time.Time,
) (application.FleetSummary, error) {
	var siteFilter any
	if siteID != nil {
		siteFilter = string(*siteID)
	}

	var summary application.FleetSummary
	err := repository.db.QueryRowContext(ctx, fleetSummaryQuery, siteFilter, onlineSince).Scan(
		&summary.Registered,
		&summary.Active,
		&summary.Inactive,
		&summary.Online,
		&summary.Stale,
		&summary.LowBattery,
		&summary.UnresolvedAlerts,
	)
	if err != nil {
		return application.FleetSummary{}, fmt.Errorf("count fleet: %w", err)
	}

	return summary, nil
}

func (repository *ReadRepository) ObservationHistory(
	ctx context.Context,
	robotID domain.RobotID,
	limit int,
) ([]application.ObservationRecord, error) {
	var exists bool
	if err := repository.db.QueryRowContext(ctx, robotExistsQuery, string(robotID)).Scan(&exists); err != nil {
		return nil, fmt.Errorf("check robot: %w", err)
	}
	if exists == false {
		return nil, application.ErrRobotNotFound
	}

	rows, err := repository.db.QueryContext(ctx, observationHistoryQuery, string(robotID), limit)
	if err != nil {
		return nil, fmt.Errorf("query history: %w", err)
	}
	defer rows.Close()

	records := make([]application.ObservationRecord, 0)
	for rows.Next() {
		var record application.ObservationRecord
		if err := rows.Scan(
			&record.ObservationID,
			&record.ObservedAt,
			&record.ReceivedAt,
			&record.BatteryPercent,
			&record.PositionX,
			&record.PositionY,
			&record.OperatingMode,
			&record.Outcome,
		); err != nil {
			return nil, fmt.Errorf("scan history: %w", err)
		}

		records = append(records, record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate history: %w", err)
	}

	return records, nil
}
