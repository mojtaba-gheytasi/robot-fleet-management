// Package bootstrap builds the running process: configuration, logger,
// database and HTTP server, and shuts them down in order on SIGINT or SIGTERM.
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/platform/config"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/platform/database"
	"github.com/mojtaba-gheytasi/robot-fleet-management/internal/platform/httpserver"
)

const shutdownTimeout = 10 * time.Second

func Run(ctx context.Context) error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	defer db.Close()

	logger.Info("connected to PostgreSQL")

	mux := http.NewServeMux()
	httpserver.RegisterHealth(mux, db)
	server := httpserver.New(mux)

	// Listen before serving, so a bad or busy address fails here with a clear
	// error instead of after "listening" has been logged.
	listener, err := net.Listen("tcp", cfg.HTTPServerAddress)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.HTTPServerAddress, err)
	}

	serverFailed := make(chan error, 1)
	go func() {
		serverFailed <- server.Serve(listener)
	}()

	logger.Info("HTTP server listening", "address", listener.Addr().String())

	select {
	case err := <-serverFailed:
		return fmt.Errorf("run HTTP server: %w", err)
	case <-ctx.Done():
	}

	logger.Info("shutting down", "timeout", shutdownTimeout.String())

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return errors.Join(fmt.Errorf("shut down HTTP server: %w", err), server.Close())
	}

	logger.Info("shutdown complete")

	return nil
}
