package main

import (
	"bytes"
	"encoding/json"
	"github.com/said-go/vibe-check/backend/internal/evaluation"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirectoryCLI(t *testing.T) {
	raw, err := os.ReadFile("../../internal/evaluation/testdata/synthetic.json")
	if err != nil {
		t.Fatal(err)
	}
	var data evaluation.Dataset
	if err = json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	write := func(name string, d evaluation.Dataset) {
		t.Helper()
		b, e := json.Marshal(d)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(dir, name), b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	write("b.json", evaluation.Dataset{Kind: data.Kind, Cases: data.Cases[1:]})
	write("a.json", evaluation.Dataset{Kind: data.Kind, Cases: data.Cases[:1]})
	invoke := func(args ...string) (int, string, string) {
		var out, err bytes.Buffer
		code := run(args, &out, &err)
		return code, out.String(), err.String()
	}
	code, out, diagnostic := invoke("-input-dir", dir, "-json")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, diagnostic)
	}
	var report evaluation.Report
	if json.Unmarshal([]byte(out), &report) != nil || report.Total != len(data.Cases) || report.Passed != len(data.Cases) || report.Outcomes[0].ID != data.Cases[0].ID {
		t.Fatalf("bad report %s", out)
	}
	bad := data.Cases[0]
	bad.Expected.Category = "impossible"
	write("a.json", evaluation.Dataset{Kind: data.Kind, Cases: []evaluation.Case{bad}})
	if code, _, _ := invoke("-input-dir", dir); code != 1 {
		t.Fatalf("mismatch exit=%d", code)
	}
	write("duplicate.json", evaluation.Dataset{Kind: data.Kind, Cases: data.Cases[:1]})
	if code, _, e := invoke("-input-dir", dir); code != 2 || !strings.Contains(e, "duplicate case id") {
		t.Fatalf("duplicate: %d %s", code, e)
	}
	for _, args := range [][]string{{}, {"-input-dir", filepath.Join(dir, "missing")}, {"-input-dir", t.TempDir()}, {"-input", "x", "-input-dir", dir}} {
		if code, _, _ := invoke(args...); code != 2 {
			t.Fatalf("args %v exit=%d", args, code)
		}
	}
	os.Remove(filepath.Join(dir, "duplicate.json"))
	os.WriteFile(filepath.Join(dir, "a.json"), []byte("{"), 0600)
	if code, _, e := invoke("-input-dir", dir); code != 2 || !strings.Contains(e, "a.json") {
		t.Fatalf("invalid file: %d %s", code, e)
	}
}
