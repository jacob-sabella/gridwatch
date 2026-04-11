// Package notifier is the opt-in push subsystem.
//
// Architecture:
//   - The store emits Transition events when a match changes status.
//   - Manager listens, converts transitions into Events (per stage, per
//     configured sink), checks against rules + dedupe state, and delivers.
//   - Each backend (Sink) returns nil on 2xx, error otherwise. Dedupe
//     state is only marked AFTER a successful delivery — fixing the RL
//     tracker race where failed deliveries got dedupe-flagged anyway.
package notifier

import (
	"context"
	"log/slog"
	"sync"

	"github.com/jsabella/gridwatch/internal/config"
	"github.com/jsabella/gridwatch/internal/model"
	"github.com/jsabella/gridwatch/internal/store"
)

// Stage identifies a notification category. The manager supports:
// "live"   — match moved from Upcoming to Live
// "result" — match moved to Final
// Future: time-based stages like "reminder_10m".
type Stage string

const (
	StageLive   Stage = "live"
	StageResult Stage = "result"
)

// Event is a single notification to deliver.
type Event struct {
	Match     model.Match
	Stage     Stage
	Title     string
	Body      string
	Click     string // optional URL to open on click
	Priority  int    // 1..5, default 3
	Tags      []string
}

// Sink is an abstract delivery backend. Implementations return nil only
// when the upstream confirms delivery (HTTP 2xx).
type Sink interface {
	Name() string
	Deliver(ctx context.Context, ev Event) error
}

// Manager owns the notifier subsystem lifecycle.
type Manager struct {
	cfg   config.Notifications
	rules *RuleEngine
	sinks []Sink
	store *store.Store
	log   *slog.Logger
}

// New constructs a Manager from configuration. If no sinks are
// configured, returns a no-op manager.
func New(cfg config.Notifications, sinks []Sink, s *store.Store, log *slog.Logger) *Manager {
	if log == nil {
		log = slog.Default()
	}
	return &Manager{
		cfg:   cfg,
		rules: NewRuleEngine(cfg.Rules),
		sinks: sinks,
		store: s,
		log:   log,
	}
}

// Enabled reports whether the manager should be started. Main only
// starts Run if this is true; otherwise the notifier is entirely dormant.
func (m *Manager) Enabled() bool {
	return m.cfg.Enabled && len(m.sinks) > 0
}

// Run consumes store transitions and delivers notifications until ctx
// is canceled. Blocking; caller typically runs it in a goroutine.
func (m *Manager) Run(ctx context.Context) error {
	if !m.Enabled() {
		<-ctx.Done()
		return ctx.Err()
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case t, ok := <-m.store.Transitions():
			if !ok {
				return nil
			}
			m.handleTransition(ctx, t)
		}
	}
}

func (m *Manager) handleTransition(ctx context.Context, t store.Transition) {
	stage := stageForTransition(t.OldStatus, t.NewStatus)
	if stage == "" {
		return
	}
	// Check dedupe: don't fire if already fired this stage.
	for _, f := range t.Match.Fired {
		if f == string(stage) {
			return
		}
	}
	// Check rules.
	if !m.rules.Matches(t.Match, stage) {
		return
	}

	ev := buildEvent(t.Match, stage)

	// Deliver to all configured sinks in parallel. Mark fired only if
	// at least one sink succeeded — missing a single broken webhook
	// shouldn't block ntfy from continuing to work.
	var wg sync.WaitGroup
	var any2xx bool
	var anyMu sync.Mutex
	for _, sink := range m.sinks {
		wg.Add(1)
		go func(sink Sink) {
			defer wg.Done()
			if err := sink.Deliver(ctx, ev); err != nil {
				m.log.Warn("notifier delivery failed",
					"sink", sink.Name(), "stage", stage, "match", t.Match.Key, "err", err)
				return
			}
			anyMu.Lock()
			any2xx = true
			anyMu.Unlock()
		}(sink)
	}
	wg.Wait()

	if any2xx {
		m.store.MarkFired(t.Match.Key, string(stage))
		m.log.Info("notifier fired",
			"stage", stage, "match", t.Match.Key,
			"title", ev.Title)
	}
}

// stageForTransition maps a status change to a notification stage.
func stageForTransition(oldS, newS model.Status) Stage {
	switch newS {
	case model.StatusLive:
		if oldS != model.StatusLive {
			return StageLive
		}
	case model.StatusFinal:
		if oldS != model.StatusFinal {
			return StageResult
		}
	}
	return ""
}

// buildEvent formats a match + stage into a human-readable ntfy event.
func buildEvent(m model.Match, stage Stage) Event {
	ev := Event{
		Match: m,
		Stage: stage,
		Click: m.PrimaryStream(),
	}
	switch stage {
	case StageLive:
		ev.Title = "🔴 LIVE · " + m.Teams[0].Name + " vs " + m.Teams[1].Name
		ev.Body = m.Tournament.Name
		if ev.Click != "" {
			ev.Body += "\nTap to watch: " + ev.Click
		}
		ev.Priority = 5
		ev.Tags = []string{"red_circle"}
	case StageResult:
		t1 := m.Teams[0]
		t2 := m.Teams[1]
		scoreStr := ""
		if t1.Score != nil && t2.Score != nil {
			scoreStr = fmtScore(*t1.Score, *t2.Score)
		}
		ev.Title = "🏆 " + t1.Name + " " + scoreStr + " " + t2.Name
		ev.Body = m.Tournament.Name
		ev.Priority = 3
		ev.Tags = []string{"trophy"}
	}
	return ev
}

func fmtScore(a, b int) string {
	return itoa(a) + "-" + itoa(b)
}

func itoa(i int) string {
	// Avoid strconv dependency in this tiny helper to keep the file self-contained.
	if i == 0 {
		return "0"
	}
	var digits [10]byte
	n := 0
	if i < 0 {
		i = -i
	}
	for i > 0 {
		digits[n] = byte('0' + i%10)
		n++
		i /= 10
	}
	out := make([]byte, n)
	for j := 0; j < n; j++ {
		out[j] = digits[n-1-j]
	}
	return string(out)
}
