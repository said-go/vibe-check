package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Environment             string
	Address                 string
	ReadTimeout             time.Duration
	WriteTimeout            time.Duration
	ShutdownTimeout         time.Duration
	MaxRequestBytes         int64
	AnalysesPerHour         int64
	AnalysesPerClientMinute int64
	MaxConcurrentAnalyses   int64
	FrontendDir             string
	AIProvider              string
	YandexAPIKey            string
	YandexFolderID          string
	YandexModel             string
	YandexBaseURL           string
	YandexTimeout           time.Duration
	OpenAIAPIKey            string
	OpenAIModel             string
	OpenAIBaseURL           string
	OpenAITimeout           time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		Environment:             env("APP_ENV", "development"),
		Address:                 env("HTTP_ADDRESS", ":"+env("PORT", "8080")),
		ReadTimeout:             duration("HTTP_READ_TIMEOUT", 10*time.Second),
		WriteTimeout:            duration("HTTP_WRITE_TIMEOUT", 120*time.Second),
		ShutdownTimeout:         duration("HTTP_SHUTDOWN_TIMEOUT", 10*time.Second),
		MaxRequestBytes:         integer("HTTP_MAX_REQUEST_BYTES", 10<<20),
		AnalysesPerHour:         integer("ANALYSES_PER_HOUR", 30),
		AnalysesPerClientMinute: integer("ANALYSES_PER_CLIENT_MINUTE", 3),
		MaxConcurrentAnalyses:   integer("MAX_CONCURRENT_ANALYSES", 2),
		FrontendDir:             env("FRONTEND_DIR", "../dist"),
		AIProvider:              env("AI_PROVIDER", "yandex"),
		YandexAPIKey:            os.Getenv("YANDEX_API_KEY"),
		YandexFolderID:          os.Getenv("YANDEX_FOLDER_ID"),
		YandexModel:             env("YANDEX_MODEL", "qwen3.6-35b-a3b"),
		YandexBaseURL:           env("YANDEX_BASE_URL", "https://ai.api.cloud.yandex.net/v1"),
		YandexTimeout:           duration("YANDEX_TIMEOUT", 90*time.Second),
		OpenAIAPIKey:            os.Getenv("OPENAI_API_KEY"),
		OpenAIModel:             os.Getenv("OPENAI_MODEL"),
		OpenAIBaseURL:           env("OPENAI_BASE_URL", "https://api.openai.com/v1"),
		OpenAITimeout:           duration("OPENAI_TIMEOUT", 45*time.Second),
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
	if c.AnalysesPerHour < 1 || c.AnalysesPerHour > 10000 || c.AnalysesPerClientMinute < 1 || c.AnalysesPerClientMinute > 1000 || c.MaxConcurrentAnalyses < 1 || c.MaxConcurrentAnalyses > 100 {
		return errors.New("analysis limits must be positive and within configured bounds")
	}
	if c.AIProvider != "yandex" && c.AIProvider != "openai" {
		return errors.New("AI_PROVIDER must be yandex or openai")
	}
	if c.FrontendDir == "" {
		return errors.New("FRONTEND_DIR is required")
	}
	if c.YandexTimeout <= 0 || c.YandexTimeout > 2*time.Minute {
		return errors.New("YANDEX_TIMEOUT must be positive and at most 2 minutes")
	}
	if c.YandexBaseURL == "" {
		return errors.New("YANDEX_BASE_URL is required")
	}
	if c.OpenAITimeout <= 0 || c.OpenAITimeout > 2*time.Minute {
		return errors.New("OPENAI_TIMEOUT must be positive and at most 2 minutes")
	}
	if c.Environment == "production" && c.AIProvider == "openai" && (c.OpenAIAPIKey == "" || c.OpenAIModel == "") {
		return errors.New("OPENAI_API_KEY and OPENAI_MODEL are required in production")
	}
	if c.Environment == "production" && c.AIProvider == "yandex" && (c.YandexAPIKey == "" || c.YandexFolderID == "") {
		return errors.New("YANDEX_API_KEY and YANDEX_FOLDER_ID are required in production")
	}
	if c.OpenAIBaseURL == "" {
		return errors.New("OPENAI_BASE_URL is required")
	}
	if c.Environment == "production" {
		baseURL := c.YandexBaseURL
		if c.AIProvider == "openai" {
			baseURL = c.OpenAIBaseURL
		}
		parsed, err := url.Parse(baseURL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
			return errors.New("AI provider URL must use HTTPS in production")
		}
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
