package httpapi

import (
	"crypto/sha256"
	"sync"
	"time"

	"github.com/said-go/vibe-check/backend/internal/scoring"
)

const (
	idempotencyTTL        = 10 * time.Minute
	idempotencyMaxEntries = 256
)

// The cache retains a digest and a scored result, never the uploaded image.
// It is deliberately bounded and scoped to one server process.
type idempotencyCache struct {
	mu      sync.Mutex
	entries map[string]*idempotencyEntry
}

type idempotencyEntry struct {
	digest    [sha256.Size]byte
	done      chan struct{}
	complete  bool
	expiresAt time.Time
	outcome   analysisOutcome
}

type analysisOutcome struct {
	result        scoring.Result
	diagnostics   *scoring.Diagnostic
	preset        bool
	analysisID    string
	visionErr     error
	scoringFailed bool
	internalError bool
	rateLimited   bool
	retryAfter    int
}

type idempotencyState uint8

const (
	idempotencyLeader idempotencyState = iota
	idempotencyExisting
	idempotencyConflict
	idempotencyFull
)

func newIdempotencyCache() *idempotencyCache {
	return &idempotencyCache{entries: make(map[string]*idempotencyEntry)}
}

func validIdempotencyKey(key string) bool {
	if len(key) < 16 || len(key) > 80 {
		return false
	}
	for _, char := range key {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-' || char == '_') {
			return false
		}
	}
	return true
}

func (c *idempotencyCache) contains(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	return ok && (!entry.complete || time.Now().Before(entry.expiresAt))
}

func (c *idempotencyCache) begin(key string, sanitizedJPEG []byte, originalHash ...string) (*idempotencyEntry, idempotencyState) {
	hash := sha256.New()
	hash.Write(sanitizedJPEG)
	if len(originalHash) > 0 {
		hash.Write([]byte("\x00" + originalHash[0]))
	}
	var digest [sha256.Size]byte
	copy(digest[:], hash.Sum(nil))
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for cachedKey, entry := range c.entries {
		if entry.complete && !now.Before(entry.expiresAt) {
			delete(c.entries, cachedKey)
		}
	}
	if entry, ok := c.entries[key]; ok {
		if entry.digest != digest {
			return nil, idempotencyConflict
		}
		return entry, idempotencyExisting
	}
	if len(c.entries) >= idempotencyMaxEntries {
		var oldestKey string
		var oldestTime time.Time
		for candidateKey, entry := range c.entries {
			if entry.complete && (oldestKey == "" || entry.expiresAt.Before(oldestTime)) {
				oldestKey, oldestTime = candidateKey, entry.expiresAt
			}
		}
		if oldestKey == "" {
			return nil, idempotencyFull
		}
		delete(c.entries, oldestKey)
	}
	entry := &idempotencyEntry{digest: digest, done: make(chan struct{})}
	c.entries[key] = entry
	return entry, idempotencyLeader
}

func (c *idempotencyCache) finish(key string, entry *idempotencyEntry, outcome analysisOutcome) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry.complete {
		return
	}
	entry.outcome = outcome
	entry.complete = true
	entry.expiresAt = time.Now().Add(idempotencyTTL)
	if outcome.visionErr != nil || outcome.scoringFailed || outcome.internalError || outcome.rateLimited {
		if c.entries[key] == entry {
			delete(c.entries, key)
		}
	}
	close(entry.done)
}
