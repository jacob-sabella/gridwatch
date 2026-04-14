package poller

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jacob-sabella/gridwatch/internal/model"
	"github.com/jacob-sabella/gridwatch/internal/store"
)

type fakeSource struct {
	name  string
	games []string
	calls int64
	delay time.Duration
	err   error
}

func (f *fakeSource) Name() string               { return f.name }
func (f *fakeSource) Games() []string            { return f.games }
func (f *fakeSource) MinInterval() time.Duration { return time.Millisecond }
func (f *fakeSource) Fetch(ctx context.Context, game string) ([]model.Match, error) {
	atomic.AddInt64(&f.calls, 1)
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if f.err != nil {
		return nil, f.err
	}
	return []model.Match{
		{
			Key: game + "_m1", Source: f.name, Game: game,
			StartTime: time.Now().Add(time.Hour),
			Teams:     [2]model.Team{{Name: "A"}, {Name: "B"}},
			Status:    model.StatusUpcoming, StatusText: "upcoming",
			Streams: []model.Stream{{Platform: "twitch", Channel: "x", URL: "https://twitch.tv/x"}},
		},
	}, nil
}

func TestPollerRunsPerGameAndMerges(t *testing.T) {
	src := &fakeSource{name: "fakesource", games: []string{"g1", "g2"}}
	s := store.New()

	p := New(Config{
		Source:   src,
		Store:    s,
		Interval: 20 * time.Millisecond,
		Jitter:   5 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)

	// Each game should have been polled ≥2 times in ~80ms with 20ms interval.
	if c := atomic.LoadInt64(&src.calls); c < 4 {
		t.Errorf("expected ≥4 fetches across 2 games, got %d", c)
	}
	// Store should have seen matches from both games.
	all := s.Query(store.FilterQuery{})
	if len(all) != 2 {
		t.Errorf("expected 2 distinct matches in store, got %d", len(all))
	}
}

func TestPollerTolerantToFetchErrors(t *testing.T) {
	src := &fakeSource{name: "fake", games: []string{"g"}, err: context.DeadlineExceeded}
	s := store.New()
	p := New(Config{Source: src, Store: s, Interval: 10 * time.Millisecond})

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)

	// Store should not have transitioned to ready because every fetch failed.
	if s.Ready() {
		t.Error("store should not be ready after all-failed polls")
	}
	// But the poller should have kept trying (counter ≥ 2).
	if c := atomic.LoadInt64(&src.calls); c < 2 {
		t.Errorf("expected ≥2 attempts, got %d", c)
	}
}

func TestPollerEnforcesMinInterval(t *testing.T) {
	src := &fakeSource{name: "fake", games: []string{"g"}}
	s := store.New()
	// User config says 1ms but source requires at least 1ms — poller should
	// upgrade to the source floor. We just check it doesn't crash.
	p := New(Config{
		Source:   src,
		Store:    s,
		Interval: 0, // too low
	})
	if p.interval < src.MinInterval() {
		t.Errorf("poller did not honor source MinInterval: %s", p.interval)
	}
}
