package httpapi

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// AttemptBudget reserves one potentially billable attempt. Reservations are not
// refunded: a network failure cannot prove that the provider did not bill it.
type AttemptBudget interface {
	Take(context.Context) (int, error)
}

type RedisBudget struct {
	client *redis.Client
	key    string
	limit  int
}

// The window starts on the first call and expires after 3600 Redis seconds.
// Read/check/increment/expiry execute atomically and use Redis TTL, not app clocks.
const budgetScript = `
local count = tonumber(redis.call('GET', KEYS[1]) or '0')
local ttl = redis.call('TTL', KEYS[1])
if count >= tonumber(ARGV[1]) then
 if ttl < 0 then return 3600 end
 if ttl == 0 then return 1 end
 return ttl
end
redis.call('INCR', KEYS[1])
if ttl < 0 then redis.call('EXPIRE', KEYS[1], 3600) end
return 0`

func NewRedisBudget(rawURL, key string, limit int) (*RedisBudget, error) {
	options, err := redis.ParseURL(rawURL)
	if err != nil || key == "" || limit < 1 {
		return nil, errors.New("invalid shared budget configuration")
	}
	options.MaxRetries = -1 // An ambiguous reply must never silently repeat INCR.
	options.DialTimeout = time.Second
	options.ReadTimeout = time.Second
	options.WriteTimeout = time.Second
	options.ContextTimeoutEnabled = true
	return &RedisBudget{redis.NewClient(options), key, limit}, nil
}

func (b *RedisBudget) Take(ctx context.Context) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	n, err := b.client.Eval(ctx, budgetScript, []string{b.key}, b.limit).Int()
	if err != nil || n < 0 || n > 3600 {
		return 0, errors.New("shared budget unavailable")
	}
	return n, nil
}
func (b *RedisBudget) Close() error { return b.client.Close() }
