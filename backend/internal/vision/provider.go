package vision

import (
	"context"
	"errors"

	"github.com/said-go/vibe-check/backend/internal/analysis"
)

var (
	ErrUnavailable     = errors.New("vision provider unavailable")
	ErrRejected        = errors.New("vision request rejected")
	ErrInvalidResponse = errors.New("invalid vision provider response")
)

type Provider interface {
	Analyze(ctx context.Context, sanitizedJPEG []byte) (analysis.VisualFeatures, error)
}

type Unconfigured struct{}

func (Unconfigured) Analyze(context.Context, []byte) (analysis.VisualFeatures, error) {
	return analysis.VisualFeatures{}, ErrUnavailable
}
