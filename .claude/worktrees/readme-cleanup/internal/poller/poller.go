// Package poller drives per-game goroutines against a source. Each
// game runs on its own tick with a jittered offset so many games
// don't stampede upstream at the same instant.
package poller

import (
	"context"
	"errors"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/jacob-sabella/gridwatch/internal/ratelimit"
	"github.com/jacob-sabella/gridwatch/internal/source"
	"github.com/jacob-sabella/gridwatch/internal/store"
)

// Poller runs one goroutine per (source, game). It owns the lifecycle
// of those goroutines and coordinates shutdown via a context.
type Poller struct {
	src      source.Source
	store    *store.Store
	interval time.Duration
	jitter   time.Duration
	log      *slog.Logger
}

// Config is what main.go passes to New.
type Config struct {
	Source   source.Source
	Store    *store.Store
	Interval time.Duration // per-game poll floor; must be ≥ Source.MinInterval()
	Jitter   time.Duration // random offset ceiling for per-game start
	Logger   *slog.Logger
}

// New validates the config and returns a Poller ready for Run.
func New(cfg Config) *Poller {
	interval := cfg.Interval
	if minInt := cfg.Source.MinInterval(); interval < minInt {
		interval = minInt
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Poller{
		src:      cfg.Source,
		store:    cfg.Store,
		interval: interval,
		jitter:   cfg.Jitter,
		log:      log,
	}
}

// Run launches a goroutine per game and blocks until ctx is canceled.
// Returns the first error observed, but only after all goroutines have
// drained — individual fetch errors are logged but do not abort the run.
func (p *Poller) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	for _, game := range p.src.Games() {
		wg.Add(1)
		go func(game string) {
			defer wg.Done()
			p.runGame(ctx, game)
		}(game)
	}
	wg.Wait()
	return ctx.Err()
}

// runGame is the per-game loop. It fetches immediately (after a jittered
// warmup), then on p.interval. Errors are logged; ErrCooldown is
// handled silently (the limiter already tracks it).
func (p *Poller) runGame(ctx context.Context, game string) {
	// Random jitter so starting N games doesn't produce a burst.
	if p.jitter > 0 {
		delay := time.Duration(rand.Int63n(int64(p.jitter)))
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return
		}
	}

	// Initial fetch.
	p.fetchOnce(ctx, game)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.fetchOnce(ctx, game)
		}
	}
}

func (p *Poller) fetchOnce(ctx context.Context, game string) {
	start := time.Now()
	matches, err := p.src.Fetch(ctx, game)
	if err != nil {
		var cd *ratelimit.ErrCooldown
		if errors.As(err, &cd) {
			p.log.Debug("poll skipped (cooldown)",
				"source", p.src.Name(), "game", game, "remaining", cd.Remaining)
			return
		}
		p.log.Warn("poll failed",
			"source", p.src.Name(), "game", game, "err", err)
		return
	}
	p.store.Merge(p.src.Name(), game, matches, time.Now().UTC())
	p.log.Debug("poll ok",
		"source", p.src.Name(), "game", game,
		"matches", len(matches),
		"elapsed", time.Since(start))
}
