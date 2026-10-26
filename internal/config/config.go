// Package config loads license server settings from environment variables.
package config

import (
	"errors"
	"os"
	"time"

	"github.com/farshadfighter/taktaplus-lic/internal/secretbox"
)

// Config holds runtime settings.
type Config struct {
	Listen          string         // TPLIC_LISTEN, default ":8080"
	DatabaseURL     string         // TPLIC_DATABASE_URL, required
	MasterKey       *secretbox.Box // TPLIC_MASTER_KEY: base64 of 32 random bytes, required
	CookieSecure    bool           // TPLIC_COOKIE_SECURE, default true; set false only for local HTTP
	PublicURL       string         // TPLIC_PUBLIC_URL: external base URL, written into licenses for heartbeats
	LicenseKeyFile  string         // TPLIC_LICENSE_KEY_FILE: encrypted signing key (lictool keygen); empty disables issuing
	LicenseKeyPass  []byte         // TPLIC_LICENSE_KEY_PASSPHRASE
	ShutdownTimeout time.Duration  // TPLIC_SHUTDOWN_TIMEOUT, default 15s
	LogLevel        string         // TPLIC_LOG_LEVEL: debug, info, warn, error
}

// FromEnv reads the configuration from the process environment.
func FromEnv() (Config, error) {
	c := Config{
		Listen:          getenv("TPLIC_LISTEN", ":8080"),
		DatabaseURL:     os.Getenv("TPLIC_DATABASE_URL"),
		CookieSecure:    os.Getenv("TPLIC_COOKIE_SECURE") != "false",
		PublicURL:       os.Getenv("TPLIC_PUBLIC_URL"),
		LicenseKeyFile:  os.Getenv("TPLIC_LICENSE_KEY_FILE"),
		LicenseKeyPass:  []byte(os.Getenv("TPLIC_LICENSE_KEY_PASSPHRASE")),
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
	key := os.Getenv("TPLIC_MASTER_KEY")
	if key == "" {
		return c, errors.New("config: TPLIC_MASTER_KEY is required (generate with: head -c32 /dev/urandom | base64)")
	}
	box, err := secretbox.FromBase64(key)
	if err != nil {
		return c, errors.New("config: TPLIC_MASTER_KEY must be base64 of 32 bytes")
	}
	c.MasterKey = box
	return c, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
