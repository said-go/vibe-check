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

func TestExpectedCoverageCLI(t *testing.T) {
	raw, err := os.ReadFile("../../internal/evaluation/testdata/synthetic.json")
	if err != nil {
		t.Fatal(err)
	}
	var d evaluation.Dataset
	if err = json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	d.Cases = d.Cases[:2]
	raw, err = json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err = os.WriteFile(filepath.Join(dir, "partial.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		count   string
		code    int
		status  string
		missing int
	}{
		{"3", 1, "incomplete", 1}, {"2", 0, "complete", 0}, {"1", 2, "", 0}, {"0", 2, "", 0}, {"-1", 2, "", 0}, {"10001", 2, "", 0},
	} {
		t.Run(tc.count, func(t *testing.T) {
			var out, diagnostic bytes.Buffer
			code := run([]string{"-input-dir", dir, "-json", "-expected-count", tc.count}, &out, &diagnostic)
			if code != tc.code {
				t.Fatalf("code %d: %s", code, diagnostic.String())
			}
			if code == 2 {
				if out.Len() != 0 {
					t.Fatal("invalid count emitted report")
				}
				return
			}
			var r evaluation.Report
			if err := json.Unmarshal(out.Bytes(), &r); err != nil {
				t.Fatal(err)
			}
			if r.Passed != 2 || r.Failed != 0 || r.CoverageStatus != tc.status || r.MissingCount == nil || *r.MissingCount != tc.missing {
				t.Fatalf("report %+v", r)
			}
		})
	}
	var out, diagnostic bytes.Buffer
	if code := run([]string{"-input-dir", dir, "-json"}, &out, &diagnostic); code != 0 {
		t.Fatal(diagnostic.String())
	}
	var r evaluation.Report
	if err := json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	if r.CoverageStatus != "unknown" || r.ExpectedCount != nil || r.MissingCount != nil {
		t.Fatalf("omitted count claims coverage: %+v", r)
	}
}

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
