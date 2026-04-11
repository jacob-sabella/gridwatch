package httpx

import (
	"errors"
	"fmt"
	"time"
)

// ErrRateLimited is returned when the upstream responds with 429.
// Consumers should back off for RetryAfter before the next attempt.
type ErrRateLimited struct {
	RetryAfter time.Duration
	Upstream   string
}

func (e *ErrRateLimited) Error() string {
	return fmt.Sprintf("rate limited by %s: retry after %s", e.Upstream, e.RetryAfter)
}

// ErrUpstream wraps a non-2xx status code from an upstream source.
type ErrUpstream struct {
	Status int
	URL    string
	Body   string
}

func (e *ErrUpstream) Error() string {
	return fmt.Sprintf("upstream %s returned %d", e.URL, e.Status)
}

// IsRetryable reports whether the error is transient (network error,
// 5xx, or rate-limited).
func IsRetryable(err error) bool {
	var rl *ErrRateLimited
	if errors.As(err, &rl) {
		return true
	}
	var us *ErrUpstream
	if errors.As(err, &us) {
		return us.Status >= 500
	}
	// Network errors — conservatively treat as retryable.
	return err != nil
}
