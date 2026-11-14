package vision

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type attemptTransport func(*http.Request) (*http.Response, error)

func (f attemptTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func invalidAttemptResponse() *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"choices":[],"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}`))}
}

func TestAttemptsShareDeadlineAndReportUsage(t *testing.T) {
	var deadlines []time.Time
	var reports []AttemptInfo
	guards := 0
	client := &http.Client{Transport: attemptTransport(func(r *http.Request) (*http.Response, error) {
		d, ok := r.Context().Deadline()
		if !ok {
			t.Fatal("no total deadline")
		}
		deadlines = append(deadlines, d)
		return invalidAttemptResponse(), nil
	})}
	ctx := WithAttemptGuard(context.Background(), func() error { guards++; return nil })
	ctx = WithAttemptObserver(ctx, func(info AttemptInfo) { reports = append(reports, info) })
	_, err := NewYandex("key", "folder", "model", "https://example.invalid", client).Analyze(ctx, []byte("jpeg"))
	if !errors.Is(err, ErrInvalidResponse) || guards != 2 || len(deadlines) != 2 || !deadlines[0].Equal(deadlines[1]) || time.Until(deadlines[0]) > AnalysisTimeout {
		t.Fatalf("err=%v guards=%d deadlines=%v", err, guards, deadlines)
	}
	if len(reports) != 2 || reports[1].HTTPStatus != 200 || !reports[1].UsageAvailable || reports[1].TotalTokens != 10 || reports[1].Outcome != "invalid_response" {
		t.Fatalf("reports=%+v", reports)
	}
}

func TestRetryRequiresTimeAndBudget(t *testing.T) {
	for _, limitedBy := range []string{"time", "budget", "cancelled"} {
		t.Run(limitedBy, func(t *testing.T) {
			calls, guards := 0, 0
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if limitedBy == "time" {
				var c context.CancelFunc
				ctx, c = context.WithTimeout(ctx, time.Second)
				defer c()
			}
			if limitedBy == "cancelled" {
				cancel()
			}
			ctx = WithAttemptGuard(ctx, func() error {
				guards++
				if limitedBy == "budget" && guards == 2 {
					return ErrRateLimited
				}
				return nil
			})
			client := &http.Client{Transport: attemptTransport(func(r *http.Request) (*http.Response, error) { calls++; return invalidAttemptResponse(), nil })}
			_, err := NewYandex("key", "folder", "model", "https://example.invalid", client).Analyze(ctx, []byte("jpeg"))
			wantCalls := 1
			wantErr := ErrInvalidResponse
			if limitedBy == "budget" {
				wantErr = ErrRateLimited
			}
			if limitedBy == "cancelled" {
				wantCalls = 0
				wantErr = context.Canceled
			}
			if calls != wantCalls || !errors.Is(err, wantErr) {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestCancellationStopsInFlightAttempt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var report AttemptInfo
	ctx = WithAttemptObserver(ctx, func(info AttemptInfo) { report = info })
	client := &http.Client{Transport: attemptTransport(func(r *http.Request) (*http.Response, error) {
		cancel()
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	_, err := NewYandex("key", "folder", "model", "https://example.invalid", client).Analyze(ctx, []byte("jpeg"))
	if !errors.Is(err, context.Canceled) || report.Outcome != "cancelled" {
		t.Fatalf("cancellation lost: %v, report=%+v", err, report)
	}
}

func TestOpenAIReportsUsageAndHonorsAttemptGuard(t *testing.T) {
	for _, allowed := range []bool{true, false} {
		t.Run(map[bool]string{true: "allowed", false: "denied"}[allowed], func(t *testing.T) {
			calls := 0
			var reports []AttemptInfo
			ctx := WithAttemptGuard(context.Background(), func() error {
				if !allowed {
					return ErrRateLimited
				}
				return nil
			})
			ctx = WithAttemptObserver(ctx, func(info AttemptInfo) { reports = append(reports, info) })
			client := &http.Client{Transport: attemptTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if deadline, ok := r.Context().Deadline(); !ok || time.Until(deadline) > AnalysisTimeout {
					t.Fatal("missing bounded AI deadline")
				}
				body, _ := json.Marshal(map[string]any{
					"output": []any{map[string]any{"content": []any{map[string]any{"type": "output_text", "text": featureJSON}}}},
					"usage":  map[string]int{"input_tokens": 11, "output_tokens": 4, "total_tokens": 15},
				})
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}, nil
			})}
			_, err := NewOpenAI("key", "model", "https://example.invalid", client).Analyze(ctx, []byte("jpeg"))
			if !allowed {
				if !errors.Is(err, ErrRateLimited) || calls != 0 || len(reports) != 0 {
					t.Fatalf("denied request reached provider: err=%v calls=%d reports=%v", err, calls, reports)
				}
				return
			}
			if err != nil || calls != 1 || len(reports) != 1 || reports[0].Outcome != "success" || reports[0].TotalTokens != 15 || !reports[0].UsageAvailable {
				t.Fatalf("err=%v calls=%d reports=%+v", err, calls, reports)
			}
		})
	}
}
