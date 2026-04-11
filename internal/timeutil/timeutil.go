// Package timeutil holds helpers for the EPG timeline math (bucket slots,
// "now" cursor, timezone normalization).
package timeutil

import "time"

// FloorSlot rounds t down to the nearest multiple of slot, assuming
// slot ≥ 1 minute. Used as the start of the EPG window.
func FloorSlot(t time.Time, slot time.Duration) time.Time {
	if slot <= 0 {
		return t
	}
	return t.Truncate(slot)
}

// SlotCount returns the number of slots in [start, end).
func SlotCount(start, end time.Time, slot time.Duration) int {
	if slot <= 0 {
		return 0
	}
	return int(end.Sub(start) / slot)
}

// SlotIndex returns the slot offset of t from the window start.
// Clamped to [0, maxSlots).
func SlotIndex(t, start time.Time, slot time.Duration, maxSlots int) int {
	if slot <= 0 {
		return 0
	}
	idx := int(t.Sub(start) / slot)
	if idx < 0 {
		return 0
	}
	if idx >= maxSlots {
		return maxSlots - 1
	}
	return idx
}

// LoadLocation returns a *time.Location, falling back to UTC if the
// name can't be resolved. (Should not happen in practice because
// config.Validate already exercised LoadLocation, but defensive.)
func LoadLocation(name string) *time.Location {
	if name == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}
