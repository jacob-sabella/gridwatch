package store

import (
	"time"

	"github.com/jacob-sabella/gridwatch/internal/model"
)

// FilterQuery is the query shape for Store.Query. Zero values mean
// "no constraint" on that axis.
//
// StatusMask is a bit set where the ith bit corresponds to Status(i).
// Use StatusMaskAll for "any status".
type FilterQuery struct {
	Games      []string
	Regions    []string
	Tiers      []string
	HasStream  bool // when true, exclude matches with no stream
	StatusMask uint8
	Window     TimeWindow
}

// StatusMaskAll is the any-status mask.
const StatusMaskAll uint8 = 0xFF

// StatusMaskOf returns a mask for the given statuses.
func StatusMaskOf(statuses ...model.Status) uint8 {
	var m uint8
	for _, s := range statuses {
		m |= 1 << uint8(s)
	}
	return m
}

// TimeWindow restricts matches to those whose StartTime falls within
// [Past, Future) relative to a reference time.
type TimeWindow struct {
	Reference time.Time
	Past      time.Duration // how far back; positive duration
	Future    time.Duration // how far forward; positive duration
}

func (q FilterQuery) matches(m model.Match) bool {
	if q.HasStream && !m.HasStream() {
		return false
	}
	if len(q.Games) > 0 && !containsStr(q.Games, m.Game) {
		return false
	}
	if len(q.Regions) > 0 && !containsStr(q.Regions, m.Tournament.Region) {
		return false
	}
	if len(q.Tiers) > 0 && !containsStr(q.Tiers, m.Tournament.Tier) {
		return false
	}
	if q.StatusMask != 0 && q.StatusMask != StatusMaskAll {
		if q.StatusMask&(1<<uint8(m.Status)) == 0 {
			return false
		}
	}
	if !q.Window.Reference.IsZero() {
		lo := q.Window.Reference.Add(-q.Window.Past)
		hi := q.Window.Reference.Add(q.Window.Future)
		if m.StartTime.Before(lo) || !m.StartTime.Before(hi) {
			return false
		}
	}
	return true
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
