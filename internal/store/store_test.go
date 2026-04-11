package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/jsabella/gridwatch/internal/model"
)

func mkMatch(key, game string, start time.Time, status model.Status, teams ...string) model.Match {
	return model.Match{
		Key:        key,
		Source:     "liquipedia",
		Game:       game,
		StartTime:  start,
		Status:     status,
		StatusText: status.String(),
		Teams:      [2]model.Team{{Name: teams[0]}, {Name: teams[1]}},
		Tournament: model.Tournament{Name: "Test Event"},
		Streams:    []model.Stream{{Platform: "twitch", Channel: "x", URL: "https://twitch.tv/x"}},
	}
}

func TestMergePreservesFirstSeenAndFired(t *testing.T) {
	s := New()
	now := time.Date(2026, 4, 11, 15, 0, 0, 0, time.UTC)
	start := now.Add(30 * time.Minute)

	m := mkMatch("k1", "rl", start, model.StatusUpcoming, "A", "B")
	m.RawBlockHash = "hash1"
	s.Merge("liquipedia", "rl", []model.Match{m}, now)

	// Drain the initial transition so we can see the next one.
	<-s.Transitions()

	// Simulate notifier marking a reminder fired.
	s.MarkFired("k1", "10m")

	// Next poll: same match, status changed to live, new hash.
	later := now.Add(32 * time.Minute)
	m2 := mkMatch("k1", "rl", start, model.StatusLive, "A", "B")
	m2.RawBlockHash = "hash2"
	s.Merge("liquipedia", "rl", []model.Match{m2}, later)

	got := s.Query(FilterQuery{})
	if len(got) != 1 {
		t.Fatalf("expected 1 match, got %d", len(got))
	}
	if !got[0].FirstSeenAt.Equal(now) {
		t.Errorf("FirstSeenAt changed: want %v, got %v", now, got[0].FirstSeenAt)
	}
	if got[0].Status != model.StatusLive {
		t.Errorf("Status not updated: got %v", got[0].Status)
	}
	if len(got[0].Fired) != 1 || got[0].Fired[0] != "10m" {
		t.Errorf("Fired lost across merge: %v", got[0].Fired)
	}

	select {
	case tr := <-s.Transitions():
		if tr.OldStatus != model.StatusUpcoming || tr.NewStatus != model.StatusLive {
			t.Errorf("unexpected transition: %+v", tr)
		}
	default:
		t.Error("expected transition emission on status change")
	}
}

func TestMergeEvictsOldMatches(t *testing.T) {
	s := New()
	now := time.Date(2026, 4, 11, 15, 0, 0, 0, time.UTC)

	old := mkMatch("k-old", "rl", now.Add(-49*time.Hour), model.StatusFinal, "A", "B")
	fresh := mkMatch("k-fresh", "rl", now.Add(1*time.Hour), model.StatusUpcoming, "C", "D")

	// First merge seeds both.
	s.Merge("liquipedia", "rl", []model.Match{old, fresh}, now)

	// Drain transitions emitted by initial inserts.
	for i := 0; i < 2; i++ {
		select {
		case <-s.Transitions():
		default:
		}
	}

	// Next merge only includes fresh. Old should be evicted (>48h old).
	s.Merge("liquipedia", "rl", []model.Match{fresh}, now)
	got := s.Query(FilterQuery{})
	if len(got) != 1 || got[0].Key != "k-fresh" {
		t.Fatalf("eviction failed: got %+v", got)
	}
}

func TestMergeDoesNotEvictAcrossGames(t *testing.T) {
	s := New()
	now := time.Date(2026, 4, 11, 15, 0, 0, 0, time.UTC)

	rl := mkMatch("k-rl", "rl", now.Add(1*time.Hour), model.StatusUpcoming, "A", "B")
	lol := mkMatch("k-lol", "lol", now.Add(2*time.Hour), model.StatusUpcoming, "X", "Y")
	s.Merge("liquipedia", "rl", []model.Match{rl}, now)
	s.Merge("liquipedia", "lol", []model.Match{lol}, now)

	// Re-merge rl only. lol match must survive because it's a different game.
	s.Merge("liquipedia", "rl", []model.Match{rl}, now)

	got := s.Query(FilterQuery{})
	if len(got) != 2 {
		t.Fatalf("cross-game eviction: got %d matches, want 2", len(got))
	}
}

func TestQueryFilters(t *testing.T) {
	s := New()
	now := time.Date(2026, 4, 11, 15, 0, 0, 0, time.UTC)

	a := mkMatch("a", "rl", now.Add(1*time.Hour), model.StatusUpcoming, "A", "B")
	a.Tournament.Region = "EU"
	a.Tournament.Tier = "S-Tier"

	b := mkMatch("b", "lol", now.Add(2*time.Hour), model.StatusLive, "X", "Y")
	b.Tournament.Region = "NA"
	b.Tournament.Tier = "A-Tier"

	c := mkMatch("c", "rl", now.Add(3*time.Hour), model.StatusUpcoming, "P", "Q")
	c.Streams = nil

	s.Merge("liquipedia", "rl", []model.Match{a, c}, now)
	s.Merge("liquipedia", "lol", []model.Match{b}, now)

	// HasStream filter
	got := s.Query(FilterQuery{HasStream: true})
	if len(got) != 2 {
		t.Errorf("HasStream: got %d want 2", len(got))
	}

	// Game filter
	got = s.Query(FilterQuery{Games: []string{"rl"}})
	if len(got) != 2 {
		t.Errorf("Games=rl: got %d want 2", len(got))
	}

	// Status filter
	got = s.Query(FilterQuery{StatusMask: StatusMaskOf(model.StatusLive)})
	if len(got) != 1 || got[0].Key != "b" {
		t.Errorf("StatusLive: got %+v", keysOf(got))
	}

	// Region filter
	got = s.Query(FilterQuery{Regions: []string{"EU"}})
	if len(got) != 1 || got[0].Key != "a" {
		t.Errorf("Region EU: got %+v", keysOf(got))
	}

	// Time window: 0–90 min future only
	got = s.Query(FilterQuery{Window: TimeWindow{Reference: now, Past: 0, Future: 90 * time.Minute}})
	if len(got) != 1 || got[0].Key != "a" {
		t.Errorf("Window: got %+v", keysOf(got))
	}
}

func keysOf(ms []model.Match) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Key
	}
	return out
}

func TestSnapshotRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snap.json")

	s1 := New()
	now := time.Date(2026, 4, 11, 15, 0, 0, 0, time.UTC)
	m := mkMatch("k1", "rl", now.Add(1*time.Hour), model.StatusUpcoming, "A", "B")
	s1.Merge("liquipedia", "rl", []model.Match{m}, now)

	if err := s1.SaveSnapshot(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	s2 := New()
	if err := s2.LoadSnapshot(path); err != nil {
		t.Fatalf("load: %v", err)
	}
	got := s2.Query(FilterQuery{})
	if len(got) != 1 || got[0].Key != "k1" {
		t.Fatalf("roundtrip lost data: %+v", got)
	}
}

func TestLoadSnapshotMissingFileIsOK(t *testing.T) {
	s := New()
	if err := s.LoadSnapshot("/does/not/exist/snap.json"); err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
}
