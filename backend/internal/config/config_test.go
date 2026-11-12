package config

import (
	"testing"
	"time"
)

func TestValidateRejectsInvalidConfiguration(t *testing.T) {
	tests := []Config{
		{Environment: "unknown", Address: ":8080", ReadTimeout: time.Second, WriteTimeout: time.Second, ShutdownTimeout: time.Second, MaxRequestBytes: 1024, OpenAIBaseURL: "https://api.openai.com/v1", OpenAITimeout: time.Second},
		{Environment: "test", Address: "", ReadTimeout: time.Second, WriteTimeout: time.Second, ShutdownTimeout: time.Second, MaxRequestBytes: 1024, OpenAIBaseURL: "https://api.openai.com/v1", OpenAITimeout: time.Second},
		{Environment: "test", Address: ":8080", ReadTimeout: 0, WriteTimeout: time.Second, ShutdownTimeout: time.Second, MaxRequestBytes: 1024, OpenAIBaseURL: "https://api.openai.com/v1", OpenAITimeout: time.Second},
		{Environment: "test", Address: ":8080", ReadTimeout: time.Second, WriteTimeout: time.Second, ShutdownTimeout: time.Second, MaxRequestBytes: 10, OpenAIBaseURL: "https://api.openai.com/v1", OpenAITimeout: time.Second},
		{Environment: "production", Address: ":8080", ReadTimeout: time.Second, WriteTimeout: time.Second, ShutdownTimeout: time.Second, MaxRequestBytes: 1024, OpenAIBaseURL: "https://api.openai.com/v1", OpenAITimeout: time.Second},
	}
	for i, cfg := range tests {
		if err := cfg.Validate(); err == nil {
			t.Fatalf("case %d: expected error", i)
		}
	}
}

func TestLoadYandexDefaultsAndProductionCredentials(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("AI_PROVIDER", "yandex")
	t.Setenv("YANDEX_API_KEY", "test-key")
	t.Setenv("YANDEX_FOLDER_ID", "test-folder")
	t.Setenv("YANDEX_MODEL", "")
	t.Setenv("YANDEX_BASE_URL", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.YandexModel != "qwen3.6-35b-a3b" || cfg.YandexBaseURL != "https://ai.api.cloud.yandex.net/v1" {
		t.Fatalf("unexpected Yandex defaults: model=%q endpoint=%q", cfg.YandexModel, cfg.YandexBaseURL)
	}
	t.Setenv("YANDEX_FOLDER_ID", "")
	if _, err := Load(); err == nil {
		t.Fatal("production must reject missing Yandex folder ID")
	}
}
