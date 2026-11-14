package evaluation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"github.com/said-go/vibe-check/backend/internal/imageproc"
	"github.com/said-go/vibe-check/backend/internal/vision"
)

type ImageCase struct {
	Path     string   `json:"path"`
	Expected Expected `json:"expected"`
}
type ImageManifest struct {
	Cases []ImageCase `json:"cases"`
}

// DecodeImages deliberately returns fixed diagnostics: paths may be private.
func DecodeImages(r io.Reader) (ImageManifest, error) {
	var m ImageManifest
	raw, e := io.ReadAll(io.LimitReader(r, (1<<20)+1))
	if e != nil || len(raw) > 1<<20 {
		return m, errors.New("cannot read manifest within 1 MiB limit")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&m) != nil || d.Decode(&struct{}{}) != io.EOF || len(m.Cases) < 1 || len(m.Cases) > 100 {
		return m, errors.New("invalid manifest; expected 1..100 cases")
	}
	token := regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	for i, c := range m.Cases {
		valid := c.Path != "" && ((c.Expected.Status == "completed" && token.MatchString(c.Expected.Category) && c.Expected.Reason == "") || (c.Expected.Status == "low_confidence" && c.Expected.Category == "" && token.MatchString(c.Expected.Reason)))
		if !valid {
			return m, fmt.Errorf("invalid manifest case %d", i+1)
		}
	}
	return m, nil
}

// Collect bypasses all photo presets and never includes file paths in its dataset.
// The provider guard bounds actual HTTP attempts, including retries.
func Collect(ctx context.Context, m ImageManifest, base string, p vision.Provider, maxCalls int) (Dataset, error) {
	out := Dataset{Kind: "reviewed_features", Cases: []Case{}}
	if maxCalls < 1 || maxCalls > 100 || len(m.Cases) == 0 || len(m.Cases) > maxCalls {
		return out, errors.New("call budget must cover cases and be within 1..100")
	}
	calls := 0
	ctx = vision.WithAttemptGuard(ctx, func() error {
		if calls >= maxCalls {
			return vision.ErrRateLimited
		}
		calls++
		return nil
	})
	for i, c := range m.Cases {
		if ctx.Err() != nil {
			return Dataset{}, errors.New("collection cancelled or timed out")
		}
		path := c.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(base, path)
		}
		f, e := os.Open(path)
		if e != nil {
			return Dataset{}, fmt.Errorf("case %d: cannot read image", i+1)
		}
		raw, e := io.ReadAll(io.LimitReader(f, (10<<20)+1))
		f.Close()
		if e != nil || len(raw) > 10<<20 {
			return Dataset{}, fmt.Errorf("case %d: image exceeds 10 MiB or cannot be read", i+1)
		}
		clean, e := imageproc.New().Sanitize(bytes.NewReader(raw), "")
		if e != nil {
			return Dataset{}, fmt.Errorf("case %d: invalid JPEG/PNG or dimensions", i+1)
		}
		features, e := p.Analyze(ctx, clean.JPEG)
		if e != nil {
			return Dataset{}, fmt.Errorf("case %d: provider failed; earlier attempts may have been billed", i+1)
		}
		if features.Validate() != nil {
			return Dataset{}, fmt.Errorf("case %d: invalid feature contract", i+1)
		}
		encoded, e := json.Marshal(features)
		if e != nil {
			return Dataset{}, errors.New("cannot encode features")
		}
		out.Cases = append(out.Cases, Case{ID: fmt.Sprintf("case-%04d", i+1), Features: encoded, Expected: c.Expected})
	}
	return out, nil
}
