package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	maxOpenConns = 20
	pingTimeout  = 5 * time.Second
)

// Open connects and pings once, so a wrong URL or a database that is down
// stops the service at startup rather than on the first request.
func Open(ctx context.Context, url string) (*sql.DB, error) {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	db.SetMaxOpenConns(maxOpenConns)

	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()

	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()

		return nil, fmt.Errorf("ping database: %w", err)
	}

	return db, nil
}
