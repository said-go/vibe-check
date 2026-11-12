package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Environment     string
	Address         string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration
	MaxRequestBytes int64
}

func Load() (Config, error) {
	cfg := Config{
		Environment:     env("APP_ENV", "development"),
		Address:         env("HTTP_ADDRESS", ":8080"),
		ReadTimeout:     duration("HTTP_READ_TIMEOUT", 10*time.Second),
		WriteTimeout:    duration("HTTP_WRITE_TIMEOUT", 20*time.Second),
		ShutdownTimeout: duration("HTTP_SHUTDOWN_TIMEOUT", 10*time.Second),
		MaxRequestBytes: integer("HTTP_MAX_REQUEST_BYTES", 1<<20),
	}
	return cfg, cfg.Validate()
}

func (c Config) Validate() error {
	if c.Environment != "development" && c.Environment != "test" && c.Environment != "production" {
		return fmt.Errorf("unsupported APP_ENV %q", c.Environment)
	}
	if c.Address == "" {
		return errors.New("HTTP_ADDRESS is required")
	}
	if c.ReadTimeout <= 0 || c.WriteTimeout <= 0 || c.ShutdownTimeout <= 0 {
		return errors.New("HTTP timeouts must be positive")
	}
	if c.MaxRequestBytes < 1024 || c.MaxRequestBytes > 10<<20 {
		return errors.New("HTTP_MAX_REQUEST_BYTES must be between 1 KiB and 10 MiB")
	}
	return nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func duration(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return -1
	}
	return parsed
}

func integer(key string, fallback int64) int64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return -1
	}
	return parsed
}
