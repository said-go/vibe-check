package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/said-go/vibe-check/backend/internal/config"
	"github.com/said-go/vibe-check/backend/internal/httpapi"
	"github.com/said-go/vibe-check/backend/internal/vision"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("invalid_configuration", "error", err)
		os.Exit(1)
	}
	var provider vision.Provider = vision.Unconfigured{}
	if cfg.AIProvider == "openai" && cfg.OpenAIAPIKey != "" && cfg.OpenAIModel != "" {
		provider = vision.NewOpenAI(cfg.OpenAIAPIKey, cfg.OpenAIModel, cfg.OpenAIBaseURL, &http.Client{Timeout: cfg.OpenAITimeout})
	}
	if cfg.AIProvider == "yandex" && cfg.YandexAPIKey != "" && cfg.YandexFolderID != "" {
		provider = vision.NewYandex(cfg.YandexAPIKey, cfg.YandexFolderID, cfg.YandexModel, cfg.YandexBaseURL, &http.Client{Timeout: cfg.YandexTimeout})
	}
	presets, err := httpapi.LoadPhotoPresets(cfg.PhotoPresetsFile)
	if err != nil {
		logger.Error("invalid_preset_configuration", "error", err)
		os.Exit(1)
	}
	logger.Info("provider_budget_configured", "scope", "process", "calls_per_hour", cfg.AnalysesPerHour, "calls_per_client_minute", cfg.AnalysesPerClientMinute, "presets_loaded", len(presets))
	api := httpapi.NewWithPresets(logger, cfg.MaxRequestBytes, provider, httpapi.Limits{
		PerHour: int(cfg.AnalysesPerHour), PerClientMinute: int(cfg.AnalysesPerClientMinute), Concurrent: int(cfg.MaxConcurrentAnalyses),
		TrustedProxyCIDRs: cfg.TrustedProxyCIDRs,
	}, presets)
	mux := http.NewServeMux()
	mux.Handle("/api/", http.StripPrefix("/api", api))
	mux.Handle("/", http.FileServer(http.Dir(cfg.FrontendDir)))
	server := &http.Server{Addr: cfg.Address, Handler: mux, ReadTimeout: cfg.ReadTimeout, WriteTimeout: cfg.WriteTimeout}
	errors := make(chan error, 1)
	go func() {
		logger.Info("server_started", "address", cfg.Address, "environment", cfg.Environment)
		errors <- server.ListenAndServe()
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	select {
	case signal := <-stop:
		logger.Info("shutdown_requested", "signal", signal.String())
	case err := <-errors:
		if err != http.ErrServerClosed {
			logger.Error("server_failed", "error", err)
			os.Exit(1)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("shutdown_failed", "error", err)
		os.Exit(1)
	}
	logger.Info("server_stopped")
}
