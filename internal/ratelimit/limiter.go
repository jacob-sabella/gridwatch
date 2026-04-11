// Package ratelimit holds the outbound request budget gating every
// upstream fetch. Two-layer design: a global envelope caps all traffic
// regardless of destination; per-host limiters apply specific floors
// (e.g., Liquipedia's ≥90s per page).
//
// Upstream rate limiting is a correctness concern, not just a nice-to-have:
// Liquipedia's Terms of Use require polite request rates and will block
// abusive clients. The limiter is the one place we enforce those rules,
// and it's unit-tested so regressions fail loud.
package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Limiter enforces both per-host and global request rates.
// Safe for concurrent use.
type Limiter struct {
	globalRPS float64
	global    *rate.Limiter

	mu       sync.Mutex
	perHost  map[string]*rate.Limiter
	floors   map[string]time.Duration // minimum interval per host
	cooldown map[string]time.Time     // host -> not-before time (from 429 / 5xx)
}

// New constructs a Limiter with the given global requests-per-second
// budget. Use SetHostFloor to configure per-host minimums before issuing
// any fetches.
func New(globalRPS float64) *Limiter {
	if globalRPS <= 0 {
		globalRPS = 0.2 // conservative default: 1 request every 5 seconds
	}
	return &Limiter{
		globalRPS: globalRPS,
		global:    rate.NewLimiter(rate.Limit(globalRPS), 1),
		perHost:   make(map[string]*rate.Limiter),
		floors:    make(map[string]time.Duration),
		cooldown:  make(map[string]time.Time),
	}
}

// SetHostFloor establishes a minimum interval between requests to a host.
// For example, Liquipedia is set to 90s per the ToU.
func (l *Limiter) SetHostFloor(host string, interval time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.floors[host] = interval
	// rate.Every translates a minimum interval into a rate limit of 1/interval.
	l.perHost[host] = rate.NewLimiter(rate.Every(interval), 1)
}

// Wait blocks until the given host has budget for one request under both
// the per-host and global envelopes. If the host is in cooldown due to a
// recent 429, Wait returns an ErrCooldown without consuming tokens so the
// caller can decide to use a cached page.
func (l *Limiter) Wait(ctx context.Context, host string) error {
	l.mu.Lock()
	if until, ok := l.cooldown[host]; ok {
		if time.Now().Before(until) {
			remaining := time.Until(until)
			l.mu.Unlock()
			return &ErrCooldown{Host: host, Remaining: remaining}
		}
		delete(l.cooldown, host)
	}
	hostLim := l.perHost[host]
	l.mu.Unlock()

	if hostLim != nil {
		if err := hostLim.Wait(ctx); err != nil {
			return err
		}
	}
	return l.global.Wait(ctx)
}

// ReportBackoff records that the host returned a 429 or 5xx; subsequent
// Wait calls will error with ErrCooldown until the given duration passes.
func (l *Limiter) ReportBackoff(host string, duration time.Duration) {
	if duration <= 0 {
		duration = 10 * time.Minute
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cooldown[host] = time.Now().Add(duration)
}

// Cooldown returns the remaining cooldown for a host, or zero if none.
func (l *Limiter) Cooldown(host string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	until, ok := l.cooldown[host]
	if !ok {
		return 0
	}
	rem := time.Until(until)
	if rem < 0 {
		return 0
	}
	return rem
}

// ErrCooldown is returned by Wait when the caller should back off rather
// than block. Poller handles this by using cached data.
type ErrCooldown struct {
	Host      string
	Remaining time.Duration
}

func (e *ErrCooldown) Error() string {
	return fmt.Sprintf("%s in cooldown for %s", e.Host, e.Remaining)
}
