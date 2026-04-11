// gridwatch — a self-hosted esports TV guide.
//
// A single binary that polls Liquipedia for multi-game match data,
// renders an EPG-style web UI, and optionally pushes notifications to
// ntfy or a webhook sink.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	// Blank-import tzdata so LoadLocation works inside distroless/static
	// containers where /usr/share/zoneinfo isn't available.
	_ "time/tzdata"

	"github.com/jsabella/gridwatch/internal/buildinfo"
	"github.com/jsabella/gridwatch/internal/config"
	"github.com/jsabella/gridwatch/internal/httpx"
	"github.com/jsabella/gridwatch/internal/notifier"
	"github.com/jsabella/gridwatch/internal/poller"
	"github.com/jsabella/gridwatch/internal/ratelimit"
	"github.com/jsabella/gridwatch/internal/server"
	"github.com/jsabella/gridwatch/internal/source/liquipedia"
	"github.com/jsabella/gridwatch/internal/store"
)

func main() {
	var (
		configPath = flag.String("config", "", "path to gridwatch.yaml")
		showVer    = flag.Bool("version", false, "print version and exit")
		demoMode   = flag.Bool("demo", false, "seed store with canned fixtures (for screenshots)")
	)
	flag.Parse()

	if *showVer {
		fmt.Printf("gridwatch %s (%s, %s)\n", buildinfo.Version, buildinfo.Commit, buildinfo.Date)
		return
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %s\n", err)
		os.Exit(2)
	}

	log := newLogger(cfg.LogLevel)
	log.Info("gridwatch starting",
		"version", buildinfo.Version,
		"config", *configPath,
		"bind", cfg.Bind,
		"games", len(cfg.Games),
		"demo", *demoMode,
	)

	s := store.New()

	// Snapshot load (optional).
	snapshotPath := filepath.Join(cfg.DataDir, "snapshot.json")
	if cfg.Snapshot.Enabled {
		if err := s.LoadSnapshot(snapshotPath); err != nil {
			log.Warn("snapshot load failed (continuing with cold state)", "err", err)
		} else {
			log.Info("snapshot loaded", "path", snapshotPath)
		}
	}

	if *demoMode {
		if err := seedDemo(s); err != nil {
			log.Error("demo seed failed", "err", err)
			os.Exit(1)
		}
		log.Info("demo mode — seeded with fixtures; upstream polling disabled")
	}

	// Wire HTTP client + rate limiter + Liquipedia source.
	limiter := ratelimit.New(cfg.Poll.GlobalRPS)
	limiter.SetHostFloor(liquipedia.Host, cfg.Poll.LiquipediaInterval)

	ua := buildinfo.UserAgent(cfg.Contact)
	client := httpx.New(ua, 20*time.Second)

	games := cfg.ResolvedGames()
	liqui := liquipedia.New(client, limiter, games)

	// Poller.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	var pollerErrCh chan error
	if !*demoMode {
		p := poller.New(poller.Config{
			Source:   liqui,
			Store:    s,
			Interval: cfg.Poll.LiquipediaInterval,
			Jitter:   cfg.Poll.Jitter,
			Logger:   log,
		})
		pollerErrCh = make(chan error, 1)
		go func() {
			pollerErrCh <- p.Run(ctx)
		}()
	}

	// Snapshot periodic saver.
	if cfg.Snapshot.Enabled {
		go runSnapshotWriter(ctx, log, s, snapshotPath, cfg.Snapshot.Interval)
	}

	// Notifier (opt-in).
	if cfg.Notifications.Enabled {
		sinks := buildSinks(cfg.Notifications.Sinks, log)
		mgr := notifier.New(cfg.Notifications, sinks, s, log)
		if mgr.Enabled() {
			log.Info("notifier enabled", "sinks", len(sinks))
			go func() {
				if err := mgr.Run(ctx); err != nil && err != context.Canceled {
					log.Warn("notifier returned", "err", err)
				}
			}()
		}
	}

	// HTTP server.
	srv, err := server.New(server.Config{
		AppConfig: cfg,
		Store:     s,
		Games:     games,
		Logger:    log,
		Version:   buildinfo.Version,
	})
	if err != nil {
		log.Error("server init failed", "err", err)
		os.Exit(1)
	}

	httpSrv := &http.Server{
		Addr:              cfg.Bind,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Info("http listening", "addr", cfg.Bind)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("http server error", "err", err)
			cancel()
		}
	}()

	// Wait for shutdown signal.
	<-ctx.Done()
	log.Info("shutdown signal received")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Warn("http shutdown error", "err", err)
	}

	// Final snapshot.
	if cfg.Snapshot.Enabled {
		if err := s.SaveSnapshot(snapshotPath); err != nil {
			log.Warn("final snapshot save failed", "err", err)
		}
	}

	if pollerErrCh != nil {
		if err := <-pollerErrCh; err != nil && err != context.Canceled {
			log.Warn("poller returned", "err", err)
		}
	}

	log.Info("gridwatch stopped")
}

// runSnapshotWriter periodically persists store state to disk so that
// cold restarts warm up quickly instead of showing an empty grid.
func runSnapshotWriter(ctx context.Context, log *slog.Logger, s *store.Store, path string, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.SaveSnapshot(path); err != nil {
				log.Warn("snapshot save failed", "err", err)
			}
		}
	}
}

// buildSinks converts config sinks into concrete Sink implementations.
// Unknown kinds are logged and skipped; config.Validate has already
// rejected them so this is defensive.
func buildSinks(specs []config.NotificationSink, log *slog.Logger) []notifier.Sink {
	out := make([]notifier.Sink, 0, len(specs))
	for _, spec := range specs {
		switch spec.Kind {
		case "ntfy":
			out = append(out, notifier.NewNtfySink(spec))
		case "webhook":
			out = append(out, notifier.NewWebhookSink(spec))
		default:
			log.Warn("unknown notifier sink kind", "kind", spec.Kind)
		}
	}
	return out
}

func newLogger(level string) *slog.Logger {
	lvl := slog.LevelInfo
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))
}
