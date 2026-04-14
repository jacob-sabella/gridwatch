// Package store holds the in-memory match state and supports merge
// semantics, revision tracking for SSE clients, and snapshot persistence.
package store

import (
	"sort"
	"sync"
	"time"

	"github.com/jacob-sabella/gridwatch/internal/model"
)

// Store is a revision-tracked, goroutine-safe match index.
//
// It intentionally does not use a database: gridwatch is designed to run
// as a single process and recover cold state from an optional JSON snapshot.
// The revision counter lets SSE clients know when to re-render without
// exposing internal state.
type Store struct {
	mu       sync.RWMutex
	matches  map[string]model.Match
	revision int64
	// firstPollComplete flips the first time Merge is called with a non-error
	// poll. Used by /readyz.
	firstPollComplete bool

	// lastSuccessfulPollAt is updated on every successful merge and used by
	// /healthz to enforce the freshness SLA.
	lastSuccessfulPollAt time.Time

	// Transition channel — notifier watches it. Buffered so slow notifiers
	// don't stall polls; we drop on full channel with a counter.
	transitions chan Transition
}

// Transition describes a status change that happened during a Merge,
// used by the notifier to emit exactly-once (per transition) notifications.
type Transition struct {
	Match      model.Match
	OldStatus  model.Status
	NewStatus  model.Status
	OccurredAt time.Time
}

// New constructs an empty Store with a transition channel of the given
// buffer size. A buffer of 256 is plenty for all reasonable polling rates.
func New() *Store {
	return &Store{
		matches:     make(map[string]model.Match),
		transitions: make(chan Transition, 256),
	}
}

// Transitions returns the receive end of the transition channel for the
// notifier. Closed on Store.Close.
func (s *Store) Transitions() <-chan Transition {
	return s.transitions
}

// Revision returns the current monotonic revision number. SSE clients
// compare against their last-seen revision to decide whether to refetch.
func (s *Store) Revision() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.revision
}

// Ready reports whether at least one successful poll has completed.
// Used by /readyz.
func (s *Store) Ready() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.firstPollComplete
}

// LastSuccessfulPoll reports the timestamp of the most recent successful
// merge. Used by /healthz to enforce the freshness SLA.
func (s *Store) LastSuccessfulPoll() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastSuccessfulPollAt
}

// Merge ingests a new batch of matches from one (source, game) combination.
//
// Semantics:
//  1. Keys not in `incoming` are kept for up to 48 hours past their start
//     time, then evicted. This preserves dedupe state across re-polls even
//     when the upstream drops a match from its window.
//  2. For each incoming match, if the key already exists: preserve
//     FirstSeenAt and Fired (notifier dedupe), update everything else.
//  3. Status transitions emit a Transition on the channel (non-blocking).
//  4. Revision is bumped at the end so SSE clients can observe the update.
func (s *Store) Merge(source, game string, incoming []model.Match, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	incomingKeys := make(map[string]struct{}, len(incoming))
	for i := range incoming {
		m := incoming[i]
		incomingKeys[m.Key] = struct{}{}

		existing, ok := s.matches[m.Key]
		if ok {
			// Preserve fields owned by the store, not the parser.
			m.FirstSeenAt = existing.FirstSeenAt
			m.Fired = existing.Fired
			// If nothing materially changed, don't bump LastUpdated.
			if m.RawBlockHash == existing.RawBlockHash {
				m.LastUpdated = existing.LastUpdated
			} else {
				m.LastUpdated = now
			}
			// Detect transition.
			if m.Status != existing.Status && m.Status != model.StatusUnknown {
				s.emitTransition(Transition{
					Match:      m,
					OldStatus:  existing.Status,
					NewStatus:  m.Status,
					OccurredAt: now,
				})
			}
		} else {
			m.FirstSeenAt = now
			m.LastUpdated = now
			// First time we've seen this match — emit an initial transition
			// from Unknown → whatever state it's in, so the notifier can
			// handle brand-new live matches.
			s.emitTransition(Transition{
				Match:      m,
				OldStatus:  model.StatusUnknown,
				NewStatus:  m.Status,
				OccurredAt: now,
			})
		}
		s.matches[m.Key] = m
	}

	// Evict old entries scoped to this (source, game). We don't touch other
	// games' state — they update on their own polling cycles.
	cutoff := now.Add(-48 * time.Hour)
	for k, m := range s.matches {
		if m.Source != source || m.Game != game {
			continue
		}
		if _, present := incomingKeys[k]; present {
			continue
		}
		if m.StartTime.Before(cutoff) {
			delete(s.matches, k)
		}
	}

	s.firstPollComplete = true
	s.lastSuccessfulPollAt = now
	s.revision++
}

// emitTransition sends on the transitions channel without blocking.
// Caller must hold s.mu.
func (s *Store) emitTransition(t Transition) {
	select {
	case s.transitions <- t:
	default:
		// Channel full; drop. Notifier is too slow or down.
		// Not fatal: the matches map still has the current state, so on
		// the next cycle we'd try again for whatever hasn't fired yet.
	}
}

// MarkFired records that the notifier successfully delivered the given
// stage for a match. Idempotent and safe to call from the notifier goroutine.
func (s *Store) MarkFired(key, stage string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.matches[key]
	if !ok {
		return
	}
	for _, f := range m.Fired {
		if f == stage {
			return
		}
	}
	m.Fired = append(m.Fired, stage)
	s.matches[key] = m
}

// Query returns matches matching the filter, sorted by StartTime ascending.
func (s *Store) Query(q FilterQuery) []model.Match {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]model.Match, 0, len(s.matches))
	for _, m := range s.matches {
		if q.matches(m) {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].StartTime.Equal(out[j].StartTime) {
			return out[i].StartTime.Before(out[j].StartTime)
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// All returns every match in the store. Primarily for snapshot persistence.
func (s *Store) All() []model.Match {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Match, 0, len(s.matches))
	for _, m := range s.matches {
		out = append(out, m)
	}
	return out
}

// Load replaces the store contents with the given slice. Used only at
// startup for snapshot recovery. Does NOT emit transitions or bump revision.
func (s *Store) Load(matches []model.Match) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.matches = make(map[string]model.Match, len(matches))
	for _, m := range matches {
		s.matches[m.Key] = m
	}
}
