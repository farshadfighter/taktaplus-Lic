// Package config loads license server settings from environment variables.
package config

import (
	"errors"
	"os"
	"time"
)

// Config holds runtime settings.
type Config struct {
	Listen          string        // TPLIC_LISTEN, default ":8080"
	DatabaseURL     string        // TPLIC_DATABASE_URL, required
	ShutdownTimeout time.Duration // TPLIC_SHUTDOWN_TIMEOUT, default 15s
	LogLevel        string        // TPLIC_LOG_LEVEL: debug, info, warn, error
}

// FromEnv reads the configuration from the process environment.
func FromEnv() (Config, error) {
	c := Config{
		Listen:          getenv("TPLIC_LISTEN", ":8080"),
		DatabaseURL:     os.Getenv("TPLIC_DATABASE_URL"),
		ShutdownTimeout: 15 * time.Second,
		LogLevel:        getenv("TPLIC_LOG_LEVEL", "info"),
	}
	if v := os.Getenv("TPLIC_SHUTDOWN_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return c, errors.New("config: TPLIC_SHUTDOWN_TIMEOUT must be a duration such as 15s")
		}
		c.ShutdownTimeout = d
	}
	if c.DatabaseURL == "" {
		return c, errors.New("config: TPLIC_DATABASE_URL is required")
	}
	return c, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
