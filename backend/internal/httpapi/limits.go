package httpapi

import (
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"time"
)

// Limits bound billable AI calls per server instance. A reverse proxy must
// enforce its own public limits when several instances are deployed.
type Limits struct {
	SharedBudget      AttemptBudget
	TrustedProxyCIDRs []netip.Prefix
	PerHour           int
	PerClientMinute   int
	Concurrent        int
}

var DefaultLimits = Limits{PerHour: 30, PerClientMinute: 3, Concurrent: 2}

type requestLimiter struct {
	limits        Limits
	active        chan struct{}
	mu            sync.Mutex
	hourStart     time.Time
	hourCount     int
	hourPending   int
	minuteStart   time.Time
	clients       map[string]int
	clientPending map[string]int
}

type admission struct {
	limiter     *requestLimiter
	key         string
	hourStart   time.Time
	minuteStart time.Time
	committed   bool
	reserved    bool
}

func newRequestLimiter(limits Limits) *requestLimiter {
	if limits.PerHour <= 0 {
		limits.PerHour = DefaultLimits.PerHour
	}
	if limits.PerClientMinute <= 0 {
		limits.PerClientMinute = DefaultLimits.PerClientMinute
	}
	if limits.Concurrent <= 0 {
		limits.Concurrent = DefaultLimits.Concurrent
	}
	return &requestLimiter{limits: limits, active: make(chan struct{}, limits.Concurrent), clients: make(map[string]int), clientPending: make(map[string]int)}
}

// acquire reserves capacity before reading the body. Invalid requests release
// their reservation without consuming the billable-call budget.
func (l *requestLimiter) acquire(r *http.Request, knownRetry ...bool) (*admission, int) {
	select {
	case l.active <- struct{}{}:
	default:
		return nil, 1
	}
	key := proxyClientKey(r, l.limits.TrustedProxyCIDRs)
	now := time.Now()
	l.mu.Lock()
	l.rollWindows(now)
	var retry int
	reserved := len(knownRetry) == 0 || !knownRetry[0]
	if reserved {
		switch {
		case l.hourCount+l.hourPending >= l.limits.PerHour:
			retry = secondsUntil(l.hourStart.Add(time.Hour), now)
		case l.clients[key]+l.clientPending[key] >= l.limits.PerClientMinute:
			retry = secondsUntil(l.minuteStart.Add(time.Minute), now)
		case l.clients[key]+l.clientPending[key] == 0 && len(l.clients)+len(l.clientPending) >= 10000:
			retry = secondsUntil(l.minuteStart.Add(time.Minute), now)
		default:
			l.hourPending++
			l.clientPending[key]++
		}
	}
	reservation := &admission{limiter: l, key: key, hourStart: l.hourStart, minuteStart: l.minuteStart, reserved: reserved}
	l.mu.Unlock()
	if retry > 0 {
		<-l.active
		return nil, retry
	}
	return reservation, 0
}

func (l *requestLimiter) rollWindows(now time.Time) {
	if l.hourStart.IsZero() || !now.Before(l.hourStart.Add(time.Hour)) {
		l.hourStart, l.hourCount, l.hourPending = now, 0, 0
	}
	if l.minuteStart.IsZero() || !now.Before(l.minuteStart.Add(time.Minute)) {
		l.minuteStart, l.clients, l.clientPending = now, make(map[string]int), make(map[string]int)
	}
}

// commit charges one valid, consented request immediately before the AI call.
func (a *admission) commit() int {
	l := a.limiter
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rollWindows(now)
	if a.reserved && l.hourStart.Equal(a.hourStart) {
		l.hourPending--
	}
	if a.reserved && l.minuteStart.Equal(a.minuteStart) {
		l.clientPending[a.key]--
		if l.clientPending[a.key] == 0 {
			delete(l.clientPending, a.key)
		}
	}
	a.committed = true
	if l.hourCount+l.hourPending >= l.limits.PerHour {
		return secondsUntil(l.hourStart.Add(time.Hour), now)
	}
	if l.clients[a.key]+l.clientPending[a.key] >= l.limits.PerClientMinute {
		return secondsUntil(l.minuteStart.Add(time.Minute), now)
	}
	l.hourCount++
	l.clients[a.key]++
	return 0
}

func (a *admission) release() {
	l := a.limiter
	if !a.committed {
		l.mu.Lock()
		if a.reserved && l.hourStart.Equal(a.hourStart) {
			l.hourPending--
		}
		if a.reserved && l.minuteStart.Equal(a.minuteStart) {
			l.clientPending[a.key]--
			if l.clientPending[a.key] == 0 {
				delete(l.clientPending, a.key)
			}
		}
		l.mu.Unlock()
	}
	<-l.active
}

// Retries use the same concurrency slot but consume their own provider budget.
func (a *admission) chargeAdditional() int {
	l := a.limiter
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rollWindows(now)
	if l.hourCount+l.hourPending >= l.limits.PerHour {
		return secondsUntil(l.hourStart.Add(time.Hour), now)
	}
	if l.clients[a.key]+l.clientPending[a.key] >= l.limits.PerClientMinute {
		return secondsUntil(l.minuteStart.Add(time.Minute), now)
	}
	l.hourCount++
	l.clients[a.key]++
	return 0
}

func clientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	if r.RemoteAddr == "" {
		return "unknown"
	}
	return r.RemoteAddr
}

func secondsUntil(deadline, now time.Time) int {
	seconds := int((deadline.Sub(now) + time.Second - 1) / time.Second)
	if seconds < 1 {
		return 1
	}
	return seconds
}

func retryAfterHeader(seconds int) string { return strconv.Itoa(seconds) }
