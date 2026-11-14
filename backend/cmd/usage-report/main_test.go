package main

import (
	"bytes"
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
