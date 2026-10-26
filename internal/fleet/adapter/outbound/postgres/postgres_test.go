//go:build integration

package postgres_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	testpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/platform/database"
)

// startPostgres starts a fresh database with every migration applied, so each
// test sees the real schema, constraints and seeded sites.
func startPostgres(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()

	migrationsDirectory, err := filepath.Abs("../../../../../migrations")
	require.NoError(t, err)

	upMigrations, err := filepath.Glob(filepath.Join(migrationsDirectory, "*.up.sql"))
	require.NoError(t, err)
	require.NotEmpty(t, upMigrations)
	sort.Strings(upMigrations)

	container, err := testpostgres.Run(
		ctx,
		"postgres:18.6-alpine",
		testpostgres.WithDatabase("fleet"),
		testpostgres.WithUsername("fleet"),
		testpostgres.WithPassword("fleet_test_password"),
		testpostgres.WithInitScripts(upMigrations...),
		testpostgres.BasicWaitStrategies(),
	)
	testcontainers.CleanupContainer(t, container)
	require.NoError(t, err)

	connectionString, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	db, err := database.Open(ctx, connectionString)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	return db
}
