package httpapi

import (
	"fmt"
	"testing"

	"github.com/said-go/vibe-check/backend/internal/vision"
)

func TestIdempotencyIncludesOriginalImageDigest(t *testing.T) {
	cache := newIdempotencyCache()
	const key = "same-request-key-123456789"
	cache.begin(key, []byte("jpeg"), "first-digest")
	if _, state := cache.begin(key, []byte("jpeg"), "second-digest"); state != idempotencyConflict {
		t.Fatal("changed original digest reused cached result")
	}
}

func TestIdempotencyCacheDeduplicatesInFlightAndRejectsKeyReuse(t *testing.T) {
	cache := newIdempotencyCache()
	const key = "idempotency-key-1234567890"
	first, state := cache.begin(key, []byte("first sanitized JPEG"))
	if state != idempotencyLeader {
		t.Fatalf("first state=%d", state)
	}
	second, state := cache.begin(key, []byte("first sanitized JPEG"))
	if state != idempotencyExisting || second != first {
		t.Fatalf("in-flight duplicate state=%d same=%t", state, second == first)
	}
	select {
	case <-second.done:
		t.Fatal("in-flight request was marked complete")
	default:
	}
	if _, state := cache.begin(key, []byte("different sanitized JPEG")); state != idempotencyConflict {
		t.Fatalf("different image state=%d", state)
	}
	cache.finish(key, first, analysisOutcome{analysisID: "original-analysis-id"})
	<-second.done
	if second.outcome.analysisID != "original-analysis-id" {
		t.Fatalf("outcome=%+v", second.outcome)
	}
	if _, state := cache.begin(key, []byte("first sanitized JPEG")); state != idempotencyExisting {
		t.Fatalf("completed retry state=%d", state)
	}
}

func TestIdempotencyCacheBoundedAndFailuresRetryable(t *testing.T) {
	cache := newIdempotencyCache()
	const key = "failed-analysis-key-123456"
	entry, _ := cache.begin(key, []byte("image"))
	cache.finish(key, entry, analysisOutcome{visionErr: vision.ErrUnavailable})
	if cache.contains(key) {
		t.Fatal("failed request should not be cached")
	}
	retry, state := cache.begin(key, []byte("image"))
	if state != idempotencyLeader {
		t.Fatalf("failed request cannot be retried: state=%d", state)
	}
	cache.finish(key, retry, analysisOutcome{analysisID: "retry"})
	for i := 0; i < idempotencyMaxEntries+20; i++ {
		key := fmt.Sprintf("distinct-idempotency-key-%04d", i)
		entry, state := cache.begin(key, []byte(key))
		if state != idempotencyLeader {
			t.Fatalf("entry %d state=%d", i, state)
		}
		cache.finish(key, entry, analysisOutcome{analysisID: key})
	}
	if len(cache.entries) > idempotencyMaxEntries {
		t.Fatalf("cache exceeded cap: %d", len(cache.entries))
	}
}
