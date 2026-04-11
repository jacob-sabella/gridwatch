// Package model defines the core domain types for gridwatch.
package model

import "time"

// Status represents the lifecycle stage of a match.
type Status int

const (
	StatusUnknown  Status = iota // couldn't parse cleanly
	StatusUpcoming               // scheduled in the future
	StatusLive                   // in progress
	StatusFinal                  // completed with final score
)

func (s Status) String() string {
	switch s {
	case StatusUpcoming:
		return "upcoming"
	case StatusLive:
		return "live"
	case StatusFinal:
		return "final"
	default:
		return "unknown"
	}
}

// Team is one side of a match. Score is nil until the match has started
// and the upstream source reports a numeric value.
type Team struct {
	Name  string `json:"name"`
	Logo  string `json:"logo,omitempty"`
	Score *int   `json:"score,omitempty"`
}

// Stream is one broadcast of a match. A match may have multiple streams
// (e.g., Twitch + YouTube, or multiple language casts).
type Stream struct {
	Platform string `json:"platform"` // "twitch" | "youtube" | "other"
	Channel  string `json:"channel"`
	URL      string `json:"url"`
}

// Tournament is the event a match belongs to. Tier and Region are
// best-effort; the parser fills them when it can.
type Tournament struct {
	Name   string `json:"name"`
	Tier   string `json:"tier,omitempty"`
	Region string `json:"region,omitempty"`
	URL    string `json:"url,omitempty"`
}

// Match is the primary unit of state. Everything in gridwatch revolves
// around a stream of these from upstream sources.
type Match struct {
	Key          string     `json:"key"`
	Source       string     `json:"source"`
	Game         string     `json:"game"`
	StartTime    time.Time  `json:"start_time"`
	Teams        [2]Team    `json:"teams"`
	Tournament   Tournament `json:"tournament"`
	BestOf       int        `json:"best_of,omitempty"`
	Status       Status     `json:"-"`
	StatusText   string     `json:"status"` // marshaled form of Status
	Streams      []Stream   `json:"streams"`
	FirstSeenAt  time.Time  `json:"first_seen_at"`
	LastUpdated  time.Time  `json:"last_updated"`
	RawBlockHash string     `json:"-"`

	// Fired tracks which notification stages have already been delivered
	// (successfully) for this match. Mutated only by the notifier after a
	// 2xx response, so failed deliveries retry on the next poll.
	Fired []string `json:"-"`
}

// PrimaryStream returns the first stream URL, or empty if none.
// Convenience for click-through in the UI.
func (m *Match) PrimaryStream() string {
	if len(m.Streams) == 0 {
		return ""
	}
	return m.Streams[0].URL
}

// HasStream reports whether the match has any associated broadcast.
// gridwatch's default filter hides matches without streams.
func (m *Match) HasStream() bool {
	return len(m.Streams) > 0
}
