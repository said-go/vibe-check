package config

import "testing"

func TestLocalDiagnosticsConfiguration(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("LOCAL_DIAGNOSTICS", "")
	t.Setenv("HTTP_ADDRESS", ":8080")
	c, err := Load()
	if err != nil || c.LocalDiagnostics {
		t.Fatalf("must be disabled by default: %v", err)
	}
	t.Setenv("LOCAL_DIAGNOSTICS", "true")
	for _, address := range []string{":8080", "0.0.0.0:8080", "[::]:8080", "example.com:8080"} {
		t.Setenv("HTTP_ADDRESS", address)
		if _, err = Load(); err == nil {
			t.Fatalf("public bind accepted %s", address)
		}
	}
	for _, address := range []string{"127.0.0.1:8080", "[::1]:8080", "localhost:8080"} {
		t.Setenv("HTTP_ADDRESS", address)
		if _, err = Load(); err != nil {
			t.Fatalf("loopback rejected %s: %v", address, err)
		}
	}
	t.Setenv("APP_ENV", "production")
	if _, err = Load(); err == nil {
		t.Fatal("production diagnostics accepted")
	}
	t.Setenv("APP_ENV", "development")
	t.Setenv("LOCAL_DIAGNOSTICS", "perhaps")
	if _, err = Load(); err == nil {
		t.Fatal("invalid flag accepted")
	}
}
