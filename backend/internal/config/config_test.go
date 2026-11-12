package config

import (
	"testing"
	"time"
)

func TestValidateRejectsInvalidConfiguration(t *testing.T) {
	tests := []Config{
		{Environment: "unknown", Address: ":8080", ReadTimeout: time.Second, WriteTimeout: time.Second, ShutdownTimeout: time.Second, MaxRequestBytes: 1024},
		{Environment: "test", Address: "", ReadTimeout: time.Second, WriteTimeout: time.Second, ShutdownTimeout: time.Second, MaxRequestBytes: 1024},
		{Environment: "test", Address: ":8080", ReadTimeout: 0, WriteTimeout: time.Second, ShutdownTimeout: time.Second, MaxRequestBytes: 1024},
		{Environment: "test", Address: ":8080", ReadTimeout: time.Second, WriteTimeout: time.Second, ShutdownTimeout: time.Second, MaxRequestBytes: 10},
	}
	for i, cfg := range tests {
		if err := cfg.Validate(); err == nil {
			t.Fatalf("case %d: expected error", i)
		}
	}
}
