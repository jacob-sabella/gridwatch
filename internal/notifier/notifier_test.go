package notifier

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jsabella/gridwatch/internal/config"
	"github.com/jsabella/gridwatch/internal/model"
	"github.com/jsabella/gridwatch/internal/store"
)

type counterSink struct {
	name     string
	err      error
	calls    int64
	events   []Event
	eventsMu chan struct{} // simple mutex-ish
}

func newCounterSink(name string) *counterSink {
	return &counterSink{name: name, eventsMu: make(chan struct{}, 1)}
}

func (c *counterSink) Name() string { return c.name }
func (c *counterSink) Deliver(ctx context.Context, ev Event) error {
	atomic.AddInt64(&c.calls, 1)
	c.eventsMu <- struct{}{}
	c.events = append(c.events, ev)
	<-c.eventsMu
	return c.err
}

func TestManagerFiresOnTransition(t *testing.T) {
	s := store.New()
	sink := newCounterSink("test")
	cfg := config.Notifications{Enabled: true}
	mgr := New(cfg, []Sink{sink}, s, nil)

	// Set up a match that transitions Upcoming → Live.
	now := time.Now().UTC()
	m := model.Match{
		Key: "k1", Source: "x", Game: "rocketleague",
		StartTime:  now.Add(5 * time.Minute),
		Teams:      [2]model.Team{{Name: "A"}, {Name: "B"}},
		Tournament: model.Tournament{Name: "Evt"},
		Status:     model.StatusUpcoming, StatusText: "upcoming",
		Streams: []model.Stream{{URL: "https://twitch.tv/x"}},
	}
	s.Merge("x", "rocketleague", []model.Match{m}, now)
	// Transition to Live.
	m.Status = model.StatusLive
	m.StatusText = "live"
	s.Merge("x", "rocketleague", []model.Match{m}, now.Add(time.Minute))

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_ = mgr.Run(ctx)

	if sink.calls < 1 {
		t.Fatalf("expected ≥1 sink call, got %d", sink.calls)
	}
	// Store should have marked fired="live" after successful delivery.
	found := s.Query(store.FilterQuery{})
	if len(found) != 1 || len(found[0].Fired) == 0 || found[0].Fired[0] != "live" {
		t.Errorf("fired state not marked: %+v", found[0].Fired)
	}
}

func TestManagerDoesNotMarkFiredOnError(t *testing.T) {
	s := store.New()
	sink := newCounterSink("broken")
	sink.err = context.DeadlineExceeded // simulate failure
	cfg := config.Notifications{Enabled: true}
	mgr := New(cfg, []Sink{sink}, s, nil)

	now := time.Now().UTC()
	m := model.Match{
		Key: "k1", Source: "x", Game: "rocketleague",
		StartTime: now,
		Teams:     [2]model.Team{{Name: "A"}, {Name: "B"}},
		Status:    model.StatusLive, StatusText: "live",
		Streams: []model.Stream{{URL: "https://twitch.tv/x"}},
	}
	s.Merge("x", "rocketleague", []model.Match{m}, now)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_ = mgr.Run(ctx)

	found := s.Query(store.FilterQuery{})
	if len(found[0].Fired) != 0 {
		t.Errorf("fired should be empty on failed delivery: %+v", found[0].Fired)
	}
}

func TestNtfySinkPostsJSON(t *testing.T) {
	var gotPayload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sink := NewNtfySink(config.NotificationSink{
		Kind: "ntfy", URL: srv.URL, Topic: "rocket-league",
		User: "jacob", Password: "pw",
	})
	ev := Event{
		Title: "🔴 LIVE · G2 vs Karmine",
		Body:  "RLCS 2026\nwatch now",
		Stage: StageLive,
		Priority: 5,
		Tags:  []string{"red_circle"},
		Click: "https://twitch.tv/rl",
	}
	if err := sink.Deliver(context.Background(), ev); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if gotPayload["topic"] != "rocket-league" {
		t.Errorf("topic: %v", gotPayload["topic"])
	}
	if gotPayload["title"] != ev.Title {
		t.Errorf("title: %v (emoji should round-trip)", gotPayload["title"])
	}
	if int(gotPayload["priority"].(float64)) != 5 {
		t.Errorf("priority: %v", gotPayload["priority"])
	}
	if gotPayload["click"] != ev.Click {
		t.Errorf("click: %v", gotPayload["click"])
	}
}

func TestNtfySinkReturnsErrorOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer srv.Close()

	sink := NewNtfySink(config.NotificationSink{Kind: "ntfy", URL: srv.URL, Topic: "x"})
	err := sink.Deliver(context.Background(), Event{Title: "t", Body: "b"})
	if err == nil {
		t.Fatal("expected error for 401")
	}
}

func TestRuleEngineDefaultOn(t *testing.T) {
	e := NewRuleEngine(nil)
	m := model.Match{Game: "rl"}
	if !e.Matches(m, StageLive) {
		t.Error("default-on should pass every event")
	}
}

func TestRuleEngineGameFilter(t *testing.T) {
	e := NewRuleEngine([]config.NotificationRule{{Games: []string{"rocketleague"}}})
	if !e.Matches(model.Match{Game: "rocketleague"}, StageLive) {
		t.Error("rule should match its own game")
	}
	if e.Matches(model.Match{Game: "dota2"}, StageLive) {
		t.Error("rule should reject other games")
	}
}

func TestRuleEngineStageFilter(t *testing.T) {
	e := NewRuleEngine([]config.NotificationRule{{Stages: []string{"live"}}})
	if !e.Matches(model.Match{Game: "x"}, StageLive) {
		t.Error("live stage should match")
	}
	if e.Matches(model.Match{Game: "x"}, StageResult) {
		t.Error("result stage should be rejected by live-only rule")
	}
}

func TestRuleEngineMinTier(t *testing.T) {
	e := NewRuleEngine([]config.NotificationRule{{MinTier: "A-Tier"}})
	pass := model.Match{Tournament: model.Tournament{Tier: "S-Tier"}}
	equal := model.Match{Tournament: model.Tournament{Tier: "A-Tier"}}
	fail := model.Match{Tournament: model.Tournament{Tier: "B-Tier"}}
	if !e.Matches(pass, StageLive) || !e.Matches(equal, StageLive) {
		t.Error("S/A-Tier should satisfy A-Tier minimum")
	}
	if e.Matches(fail, StageLive) {
		t.Error("B-Tier should not satisfy A-Tier minimum")
	}
}
