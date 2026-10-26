package config

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	defaultHTTPServerAddress  = "127.0.0.1:8080"
	defaultAlertCheckInterval = 5 * time.Second
)

type Config struct {
	DatabaseURL        string
	HTTPServerAddress  string
	AlertCheckInterval time.Duration
}

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

	cfg.AlertCheckInterval = defaultAlertCheckInterval
	if value := strings.TrimSpace(getenv("ALERT_CHECK_INTERVAL")); value != "" {
		interval, err := time.ParseDuration(value)
		if err != nil || interval <= 0 {
			return Config{}, fmt.Errorf("ALERT_CHECK_INTERVAL must be a positive duration such as 5s, got %q", value)
		}
		cfg.AlertCheckInterval = interval
	}

	return cfg, nil
}
