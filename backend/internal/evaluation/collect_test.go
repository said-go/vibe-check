package evaluation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/said-go/vibe-check/backend/internal/analysis"
	"github.com/said-go/vibe-check/backend/internal/vision"
)

type collectProvider struct {
	calls    int
	fail     bool
	features analysis.VisualFeatures
}

type collectTransport func(*http.Request) (*http.Response, error)

func (t collectTransport) RoundTrip(r *http.Request) (*http.Response, error) { return t(r) }
func TestCollectBoundsProviderRetries(t *testing.T) {
	dir := t.TempDir()
	var b bytes.Buffer
	png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 128, 128)))
	os.WriteFile(filepath.Join(dir, "photo.png"), b.Bytes(), 0600)
	for _, budget := range []int{1, 2} {
		calls := 0
		client := &http.Client{Transport: collectTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[]}`)), Header: make(http.Header)}, nil
		})}
		p := vision.NewYandex("key", "folder", "model", "https://example.invalid", client)
		m := ImageManifest{Cases: []ImageCase{{Path: "photo.png", Expected: Expected{Status: "completed", Category: "goth"}}}}
		_, err := Collect(context.Background(), m, dir, p, budget)
		if err == nil || calls != budget {
			t.Fatalf("budget=%d calls=%d err=%v", budget, calls, err)
		}
	}
}

func (p *collectProvider) Analyze(ctx context.Context, b []byte) (analysis.VisualFeatures, error) {
	p.calls++
	if len(b) < 2 || b[0] != 255 || b[1] != 216 {
		panic("not sanitized JPEG")
	}
	if p.fail {
		return p.features, errors.New("secret path key should not escape")
	}
	return p.features, nil
}
func TestCollectSanitizesAndOmitsPaths(t *testing.T) {
	dir := t.TempDir()
	var b bytes.Buffer
	png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 128, 128)))
	os.WriteFile(filepath.Join(dir, "private-name.png"), b.Bytes(), 0600)
	fixture, e := os.Open("testdata/synthetic.json")
	if e != nil {
		t.Fatal(e)
	}
	defer fixture.Close()
	d, e := Decode(fixture)
	if e != nil {
		t.Fatal(e)
	}
	features, e := analysis.DecodeFeatures(bytes.NewReader(d.Cases[0].Features))
	if e != nil {
		t.Fatal(e)
	}
	p := &collectProvider{features: features}
	m := ImageManifest{Cases: []ImageCase{{Path: "private-name.png", Expected: d.Cases[0].Expected}}}
	got, e := Collect(context.Background(), m, dir, p, 1)
	if e != nil {
		t.Fatal(e)
	}
	encoded, _ := json.Marshal(got)
	if bytes.Contains(encoded, []byte("private-name")) || got.Cases[0].ID != "case-0001" || p.calls != 1 {
		t.Fatal("unsafe output or unexpected calls")
	}
	if _, e = Decode(bytes.NewReader(encoded)); e != nil {
		t.Fatal(e)
	}
	p.fail = true
	_, e = Collect(context.Background(), m, dir, p, 1)
	if e == nil || strings.Contains(e.Error(), "secret") {
		t.Fatal("provider error leaked")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := p.calls
	_, e = Collect(ctx, m, dir, p, 1)
	if e == nil || p.calls != before {
		t.Fatal("cancelled collection called provider")
	}
	_, e = Collect(context.Background(), m, dir, p, 0)
	if e == nil || p.calls != before {
		t.Fatal("invalid budget called provider")
	}
}
func TestDecodeImagesRejectsUnsafeExpectations(t *testing.T) {
	for _, raw := range []string{`{"cases":[]}`, `{"cases":[{"path":"x","expected":{"status":"completed","category":"private/name"}}]}`, `{"cases":[{"path":"x","extra":true,"expected":{"status":"completed","category":"goth"}}]}`} {
		if _, e := DecodeImages(strings.NewReader(raw)); e == nil {
			t.Fatal("accepted invalid manifest")
		}
	}
}
