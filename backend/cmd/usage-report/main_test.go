package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIStatusesAndPrivacy(t *testing.T) {
	for _, tc := range []struct {
		args  []string
		input string
		code  int
	}{
		{nil, `{"msg":"provider_attempt","provider":"yandex","outcome":"timeout"}`, 0},
		{nil, "secret malformed", 1},
		{[]string{"-input", "nonexistent-secret-path"}, "", 2},
		{[]string{"-secret-flag"}, "", 2},
	} {
		var out, err bytes.Buffer
		if code := run(tc.args, strings.NewReader(tc.input), &out, &err); code != tc.code {
			t.Fatalf("code%d want%d", code, tc.code)
		}
		if strings.Contains(out.String()+err.String(), "secret") {
			t.Fatal("raw input leaked")
		}
	}
}

func TestCLITariff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tariff.json")
	if err := os.WriteFile(path, []byte(`{"version":"synthetic-v1","currency":"RUB","rates":[{"provider":"yandex","model":"test-model","input_rub_per_million":"1","output_rub_per_million":"2"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		model, input string
		code         int
	}{
		{"test-model", `{"msg":"provider_attempt","provider":"yandex","outcome":"success","usage_available":true,"input_tokens":1000000,"output_tokens":1000000,"total_tokens":2000000}`, 0},
		{"test-model", `{"msg":"provider_attempt","provider":"yandex","outcome":"timeout"}`, 1},
		{"", ``, 2},
		{"test-model", `{"msg":"provider_attempt","provider":"yandex","model":"different","outcome":"timeout"}`, 2},
	} {
		var out, diagnostic bytes.Buffer
		args := []string{"-tariff", path}
		if tc.model != "" {
			args = append(args, "-assume-model", tc.model)
		}
		if got := run(args, strings.NewReader(tc.input), &out, &diagnostic); got != tc.code {
			t.Fatalf("code%d want%d: %s", got, tc.code, diagnostic.String())
		}
		if tc.code == 0 && !strings.Contains(out.String(), `"known_cost_rub": "3.000000000000"`) {
			t.Fatal(out.String())
		}
	}
}
