// Package config reads the service settings from environment variables.
// Docker Compose loads them from .env; .env.example documents every one.
package config

import (
	"errors"
	"strings"
)

const defaultHTTPServerAddress = "127.0.0.1:8080"

type Config struct {
	DatabaseURL       string
	HTTPServerAddress string
}

// Load takes the lookup function as a parameter so tests do not have to
// change the real environment.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		DatabaseURL:       strings.TrimSpace(getenv("DATABASE_URL")),
		HTTPServerAddress: strings.TrimSpace(getenv("HTTP_SERVER_ADDRESS")),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}

	if cfg.HTTPServerAddress == "" {
		cfg.HTTPServerAddress = defaultHTTPServerAddress
	}

	return cfg, nil
}
