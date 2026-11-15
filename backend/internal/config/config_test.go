package config

import (
	"strings"
	"testing"
	"time"
)

func TestRedisBudgetConfiguration(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	for _, raw := range []string{"not-url", "https://host", "redis://host?max_retries=5"} {
		t.Setenv("REDIS_BUDGET_URL", raw)
		if _, err := Load(); err == nil {
			t.Fatal("accepted invalid Redis config")
		}
	}
	t.Setenv("REDIS_BUDGET_URL", "redis://localhost:6379/0")
	c, err := Load()
	if err != nil || c.SharedAnalysesPerHour != 30 {
		t.Fatal("default shared config", err)
	}
	t.Setenv("APP_ENV", "production")
	t.Setenv("YANDEX_API_KEY", "test")
	t.Setenv("YANDEX_FOLDER_ID", "test")
	if _, err := Load(); err == nil {
		t.Fatal("production accepted plaintext Redis")
	}
	t.Setenv("REDIS_BUDGET_URL", "rediss://user:private@host:6379/0")
	if _, err := Load(); err != nil {
		t.Fatal("TLS config rejected", err)
	}
}

func TestTrustedProxyConfiguration(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	for _, raw := range []string{"bad", "0.0.0.0/0", "::/0", "10.0.0.1/32,", "::ffff:10.0.0.1/128", strings.Repeat("x", 4097)} {
		t.Setenv("TRUSTED_PROXY_CIDRS", raw)
		if _, err := Load(); err == nil {
			t.Fatalf("accepted invalid proxy config %q", raw)
		}
	}
	t.Setenv("TRUSTED_PROXY_CIDRS", "10.0.0.1/32, 2001:db8::/64")
	c, err := Load()
	if err != nil || len(c.TrustedProxyCIDRs) != 2 {
		t.Fatalf("valid config: %v", err)
	}
	t.Setenv("TRUSTED_PROXY_CIDRS", "")
	c, err = Load()
	if err != nil || len(c.TrustedProxyCIDRs) != 0 {
		t.Fatal("default must disable proxy trust")
	}
}

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
	t.Setenv("PHOTO_PRESETS_FILE", "")
	t.Setenv("APP_ENV", "production")
	t.Setenv("AI_PROVIDER", "yandex")
	t.Setenv("HTTP_READ_TIMEOUT", "")
	t.Setenv("HTTP_WRITE_TIMEOUT", "")
	t.Setenv("YANDEX_TIMEOUT", "")
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
	if cfg.ReadTimeout != 30*time.Second || cfg.YandexTimeout != 90*time.Second || cfg.WriteTimeout != 150*time.Second {
		t.Fatalf("inconsistent request timeouts: read=%s AI=%s write=%s", cfg.ReadTimeout, cfg.YandexTimeout, cfg.WriteTimeout)
	}
	t.Setenv("YANDEX_FOLDER_ID", "")
	if _, err := Load(); err == nil {
		t.Fatal("production must reject missing Yandex folder ID")
	}
}

func TestProductionAllowsPrivatePresetsWithoutAICredentials(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("AI_PROVIDER", "yandex")
	t.Setenv("YANDEX_API_KEY", "")
	t.Setenv("YANDEX_FOLDER_ID", "")
	t.Setenv("PHOTO_PRESETS_FILE", "private/synthetic.json")
	cfg, err := Load()
	if err != nil || cfg.PhotoPresetsFile != "private/synthetic.json" {
		t.Fatalf("private presets configuration rejected: %v", err)
	}
}

func TestLoadPortFallbackAndRateLimits(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("HTTP_ADDRESS", "")
	t.Setenv("PORT", "8089")
	t.Setenv("HTTP_READ_TIMEOUT", "")
	t.Setenv("ANALYSES_PER_HOUR", "40")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Address != ":8089" || cfg.AnalysesPerHour != 40 || cfg.ReadTimeout != 30*time.Second {
		t.Fatalf("unexpected config: address=%q perHour=%d readTimeout=%s", cfg.Address, cfg.AnalysesPerHour, cfg.ReadTimeout)
	}
	t.Setenv("HTTP_ADDRESS", "127.0.0.1:8090")
	cfg, err = Load()
	if err != nil || cfg.Address != "127.0.0.1:8090" {
		t.Fatalf("HTTP_ADDRESS did not take precedence: %q %v", cfg.Address, err)
	}
	t.Setenv("ANALYSES_PER_HOUR", "0")
	if _, err := Load(); err == nil {
		t.Fatal("zero analysis budget must be rejected")
	}
}

func TestProductionRejectsPlaintextAIEndpoint(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("AI_PROVIDER", "yandex")
	t.Setenv("YANDEX_API_KEY", "test-key")
	t.Setenv("YANDEX_FOLDER_ID", "test-folder")
	t.Setenv("YANDEX_BASE_URL", "http://ai.example.invalid/v1")
	if _, err := Load(); err == nil {
		t.Fatal("production must reject plaintext AI endpoint")
	}
}

func TestYDBBudgetDefaultsAndOverrides(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("REDIS_BUDGET_URL", "")
	t.Setenv("YDB_CONNECTION_STRING", "grpcs://ydb.serverless.yandexcloud.net:2135/?database=/ru-central1/folder/db")
	t.Setenv("YDB_BUDGET_KEY", "")
	t.Setenv("SHARED_ANALYSES_PER_HOUR", "")
	t.Setenv("SHARED_ANALYSES_PER_MONTH", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.YDBBudgetKey != "vibe-check-provider-budget" || cfg.SharedAnalysesPerHour != 30 || cfg.SharedAnalysesPerMonth != 100 {
		t.Fatal("incorrect YDB defaults")
	}
	t.Setenv("YDB_BUDGET_KEY", "release:budget_2-test")
	t.Setenv("SHARED_ANALYSES_PER_HOUR", "7")
	t.Setenv("SHARED_ANALYSES_PER_MONTH", "50")
	cfg, err = Load()
	if err != nil || cfg.YDBBudgetKey != "release:budget_2-test" || cfg.SharedAnalysesPerHour != 7 || cfg.SharedAnalysesPerMonth != 50 {
		t.Fatalf("YDB overrides: %v", err)
	}
}

func TestYDBBudgetRejectsInvalidConfiguration(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("REDIS_BUDGET_URL", "")
	t.Setenv("YDB_CONNECTION_STRING", "grpcs://host:2135/?database=/db")
	t.Setenv("YDB_BUDGET_KEY", "budget")
	t.Setenv("SHARED_ANALYSES_PER_HOUR", "30")
	t.Setenv("SHARED_ANALYSES_PER_MONTH", "100")
	cases := []struct{ key, value string }{
		{"YDB_CONNECTION_STRING", "not-url"},
		{"YDB_CONNECTION_STRING", "grpc://host:2135/?database=/db"},
		{"YDB_CONNECTION_STRING", "grpcs://user:PRIVATE@host:2135/?database=/db"},
		{"YDB_CONNECTION_STRING", "grpcs://host:2135/path?database=/db"},
		{"YDB_CONNECTION_STRING", "grpcs://host:2135/?database="},
		{"YDB_CONNECTION_STRING", "grpcs://host:2135/?database=///"},
		{"YDB_CONNECTION_STRING", "grpcs://host:2135/?database=db"},
		{"YDB_CONNECTION_STRING", "grpcs://host:2135/?database=/db&token=PRIVATE"},
		{"YDB_CONNECTION_STRING", "grpcs://host:2135/?database=/db&database=/other"},
		{"YDB_CONNECTION_STRING", "grpcs://host:2135/?database=/db#PRIVATE"},
		{"YDB_CONNECTION_STRING", "grpcs://host:2135/?database=/db,OTHER=value"},
		{"YDB_CONNECTION_STRING", "grpcs://host/?database=/db"},
		{"YDB_CONNECTION_STRING", "grpcs://host:0/?database=/db"},
		{"YDB_CONNECTION_STRING", "grpcs://host:65536/?database=/db"},
		{"YDB_CONNECTION_STRING", "grpcs://host:2135/?database=/" + strings.Repeat("x", 1024)},
		{"YDB_BUDGET_KEY", "bad/key"}, {"YDB_BUDGET_KEY", "bad key"},
		{"YDB_BUDGET_KEY", "bad,key=value"}, {"YDB_BUDGET_KEY", strings.Repeat("x", 129)},
		{"SHARED_ANALYSES_PER_HOUR", "0"}, {"SHARED_ANALYSES_PER_HOUR", "1000001"},
		{"SHARED_ANALYSES_PER_MONTH", "0"}, {"SHARED_ANALYSES_PER_MONTH", "-1"},
		{"SHARED_ANALYSES_PER_MONTH", "1000001"}, {"SHARED_ANALYSES_PER_MONTH", "NaN"},
		{"REDIS_BUDGET_URL", "rediss://user:PRIVATE@host:6379"},
	}
	for _, tc := range cases {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			_, err := Load()
			if err == nil {
				t.Fatal("accepted invalid YDB configuration")
			}
			if !strings.Contains(err.Error(), "YDB") || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatalf("unsafe/unrelated error: %v", err)
			}
		})
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.YDBBudgetKey = ""
	if cfg.Validate() == nil {
		t.Fatal("empty key accepted by Validate")
	}
	cfg.YDBBudgetKey = strings.Repeat("a", 128)
	cfg.SharedAnalysesPerHour = 1000000
	cfg.SharedAnalysesPerMonth = 1000000
	if err = cfg.Validate(); err != nil {
		t.Fatalf("valid upper bounds rejected: %v", err)
	}
}
