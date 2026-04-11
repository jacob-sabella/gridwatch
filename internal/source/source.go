// Package source defines the pluggable upstream data-source interface.
// Liquipedia is the only implementation shipped in v0.1; future sources
// (start.gg, pandascore, ...) can land here without changing the rest
// of gridwatch.
package source

import (
	"context"
	"sync"
	"time"

	"github.com/jacob-sabella/gridwatch/internal/model"
)

// Source is implemented by anything that fetches matches for one or more
// games. A source is expected to be stateless with respect to polling —
// caching and rate limiting live outside this interface, so implementations
// stay simple.
type Source interface {
	// Name uniquely identifies the source (e.g., "liquipedia"). Two different
	// sources must not share a name; the store uses (source, game) as a
	// merge scope.
	Name() string

	// Games returns the list of game slugs this source knows how to fetch.
	// Drive each game on its own polling goroutine.
	Games() []string

	// Fetch pulls matches for a single game. Implementations must honor
	// the context deadline. The caller passes a fresh slice on each call;
	// returning a nil error with a zero-length slice means "no matches
	// right now" (not an error).
	Fetch(ctx context.Context, game string) ([]model.Match, error)

	// MinInterval returns the polite floor between two fetches for the
	// same game. The poller will never go faster than this, even if the
	// user misconfigures the interval.
	MinInterval() time.Duration
}

// Registry is a goroutine-safe map of name → Source. Used by main.go for
// wiring.
type Registry struct {
	mu      sync.RWMutex
	sources map[string]Source
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{sources: make(map[string]Source)}
}

// Register adds a source under its declared Name. Duplicate names panic
// because this is a programming error, not a runtime condition.
func (r *Registry) Register(s Source) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.sources[s.Name()]; dup {
		panic("source name already registered: " + s.Name())
	}
	r.sources[s.Name()] = s
}

// Get returns a source by name, or nil if not registered.
func (r *Registry) Get(name string) Source {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sources[name]
}

// All returns every registered source.
func (r *Registry) All() []Source {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Source, 0, len(r.sources))
	for _, s := range r.sources {
		out = append(out, s)
	}
	return out
}
