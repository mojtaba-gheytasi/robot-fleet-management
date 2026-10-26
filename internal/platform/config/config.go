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
