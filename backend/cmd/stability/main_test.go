package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/said-go/vibe-check/backend/internal/evaluation"
	"github.com/said-go/vibe-check/backend/internal/imageproc"
	"github.com/said-go/vibe-check/backend/internal/stability"
)

func TestInvalidInputsDoNotEchoPrivateArguments(t *testing.T) {
	for _, args := range [][]string{
		{"prepare", "-input", "private-person-missing.json", "-output-dir", "private-person-output"},
		{"compare", "-input-dir", "private-person-missing"},
		{"compare", "-private-person-invalid"},
		{"compare", "-input", "private-person-missing.json"},
	} {
		var out, err bytes.Buffer
		if code := run(args, &out, &err); code != 2 {
			t.Fatalf("code %d", code)
		}
		if strings.Contains(out.String()+err.String(), "private-person") {
			t.Fatal("private argument leaked")
		}
	}
}

func TestOfflinePrepareAndCompareWorkflow(t *testing.T) {
	root := t.TempDir()
	write := func(name string, value []byte) string {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, value, 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	var pngBytes bytes.Buffer
	if err := png.Encode(&pngBytes, image.NewRGBA(image.Rect(0, 0, 256, 256))); err != nil {
		t.Fatal(err)
	}
	write("synthetic.png", pngBytes.Bytes())
	manifest := write("source.json", []byte(`{"cases":[{"path":"synthetic.png","expected":{"status":"completed","category":"healthy_casual"}}]}`))
	output := filepath.Join(root, "variants")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"prepare", "-input", manifest, "-output-dir", output}, &stdout, &stderr); code != 0 {
		t.Fatalf("prepare %d: %s", code, stderr.String())
	}
	encoded, err := os.ReadFile(filepath.Join(output, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := evaluation.DecodeImages(bytes.NewReader(encoded))
	if err != nil || len(m.Cases) != 5 {
		t.Fatalf("collector manifest: %+v %v", m, err)
	}
	for _, c := range m.Cases {
		raw, err := os.ReadFile(filepath.Join(output, c.Path))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := imageproc.New().Sanitize(bytes.NewReader(raw), ""); err != nil {
			t.Fatal(err)
		}
	}
	fixture, err := os.Open("../../internal/evaluation/testdata/synthetic.json")
	if err != nil {
		t.Fatal(err)
	}
	defer fixture.Close()
	source, err := evaluation.Decode(fixture)
	if err != nil {
		t.Fatal(err)
	}
	d := evaluation.Dataset{Kind: "synthetic"}
	for i := range m.Cases {
		c := source.Cases[0]
		c.ID = fmt.Sprintf("case-%04d", i+1)
		d.Cases = append(d.Cases, c)
	}
	check := func(want int) {
		t.Helper()
		raw, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		path := write("features.json", raw)
		stdout.Reset()
		stderr.Reset()
		if code := run([]string{"compare", "-input", path}, &stdout, &stderr); code != want {
			t.Fatalf("compare code %d want %d: %s", code, want, stderr.String())
		}
		var report stability.Report
		if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
			t.Fatal(err)
		}
		if report.Stable != (want == 0) || len(report.Variants) != 4 {
			t.Fatalf("wrong report: %+v", report)
		}
	}
	check(0)
	var features map[string]any
	if err := json.Unmarshal(d.Cases[1].Features, &features); err != nil {
		t.Fatal(err)
	}
	features["person_count"] = 0
	d.Cases[1].Features, _ = json.Marshal(features)
	check(1)
}
