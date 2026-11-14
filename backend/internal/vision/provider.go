package vision

import (
	"context"
	"errors"
	"time"

	"github.com/said-go/vibe-check/backend/internal/analysis"
)

var (
	ErrUnavailable     = errors.New("vision provider unavailable")
	ErrRejected        = errors.New("vision request rejected")
	ErrInvalidResponse = errors.New("invalid vision provider response")
	ErrRateLimited     = errors.New("vision attempt budget exhausted")
)

const AnalysisTimeout = 110 * time.Second

type attemptContextKey uint8

const (
	attemptGuardKey attemptContextKey = iota
	attemptObserverKey
)

// AttemptInfo deliberately excludes request bodies, credentials and model text.
type AttemptInfo struct {
	Provider                               string
	Outcome                                string
	HTTPStatus                             int
	Duration                               time.Duration
	InputTokens, OutputTokens, TotalTokens int
	UsageAvailable                         bool
}

func attemptOutcome(err error) string {
	switch {
	case err == nil:
		return "success"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, ErrRejected):
		return "rejected"
	case errors.Is(err, ErrUnavailable):
		return "unavailable"
	default:
		return "invalid_response"
	}
}

func WithAttemptGuard(ctx context.Context, guard func() error) context.Context {
	return context.WithValue(ctx, attemptGuardKey, guard)
}
func WithAttemptObserver(ctx context.Context, observer func(AttemptInfo)) context.Context {
	return context.WithValue(ctx, attemptObserverKey, observer)
}
func beforeAttempt(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if guard, ok := ctx.Value(attemptGuardKey).(func() error); ok && guard != nil {
		return guard()
	}
	return nil
}
func observeAttempt(ctx context.Context, info AttemptInfo, started time.Time) {
	info.Duration = time.Since(started)
	if observer, ok := ctx.Value(attemptObserverKey).(func(AttemptInfo)); ok && observer != nil {
		observer(info)
	}
}

type Provider interface {
	Analyze(ctx context.Context, sanitizedJPEG []byte) (analysis.VisualFeatures, error)
}

type Unconfigured struct{}

func (Unconfigured) Analyze(context.Context, []byte) (analysis.VisualFeatures, error) {
	return analysis.VisualFeatures{}, ErrUnavailable
}
