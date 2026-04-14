package liquipedia

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jacob-sabella/gridwatch/internal/model"
)

// -update regenerates golden files. Runs as:
//
//	go test ./internal/source/liquipedia/ -run TestParseGolden -update
var update = flag.Bool("update", false, "regenerate golden files")

// Reference time for deterministic tests — matches the snapshot capture
// moment so Live/Final classification is reproducible.
var testNow = time.Date(2026, 4, 11, 14, 50, 0, 0, time.UTC)

func loadSnapshot(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	html, err := DecodeResponse(raw)
	if err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	return html
}

func TestParseRocketLeagueGolden(t *testing.T) {
	html := loadSnapshot(t, "rocketleague_matches.json")
	got := Parse(html, "rocketleague", testNow, 5, 90*time.Minute)

	if len(got) == 0 {
		t.Fatal("parser returned zero matches from a non-empty snapshot")
	}

	goldenPath := filepath.Join("testdata", "rocketleague_matches.golden.json")
	if *update {
		writeGolden(t, goldenPath, got)
		return
	}

	expected := readGolden(t, goldenPath)
	compareMatches(t, expected, got)
}

func TestParseDropsBlocksWithoutStreams(t *testing.T) {
	// A block with a timestamp and teams but no Special:Stream link — must
	// be dropped, since gridwatch only tracks watchable matches.
	noStream := `` +
		`<div class="match-info">` +
		`<span class="timer-object" data-timestamp="1775923200">x</span>` +
		`<div class="block-team"><a title="Team A">A</a></div>` +
		`<div class="block-team"><a title="Team B">B</a></div>` +
		`<span class="match-info-header-scoreholder-upper">vs</span>` +
		`<div class="match-info-tournament-name"><span>Test Event</span></div>` +
		`</div>`
	got := Parse(noStream, "rocketleague", testNow, 5, 90*time.Minute)
	if len(got) != 0 {
		t.Errorf("expected 0 matches (no stream), got %d", len(got))
	}
}

func TestParseDropsMalformedBlock(t *testing.T) {
	// Missing timestamp.
	bad := `<div class="match-info">no timestamp here</div>`
	got := Parse(bad, "rocketleague", testNow, 5, 90*time.Minute)
	if len(got) != 0 {
		t.Errorf("expected 0 matches (no timestamp), got %d", len(got))
	}
}

func TestParseHandlesHTMLEntities(t *testing.T) {
	block := `<div class="match-info">` +
		`<span class="timer-object" data-timestamp="1775923200">x</span>` +
		`<div class="block-team"><a title="We don&#39;t know">WDK</a></div>` +
		`<div class="block-team"><a title="A &amp; B">A&amp;B</a></div>` +
		`<span class="match-info-header-scoreholder-upper">vs</span>` +
		`<div class="match-info-tournament-name"><span>Test &amp; Event</span></div>` +
		`<a href="/rocketleague/Special:Stream/twitch/Rocket_League" title="Special:Stream/twitch/Rocket_League"></a>` +
		`</div>`
	got := Parse(block, "rocketleague", testNow, 5, 90*time.Minute)
	if len(got) != 1 {
		t.Fatalf("expected 1 match, got %d", len(got))
	}
	m := got[0]
	if m.Teams[0].Name != "We don't know" {
		t.Errorf("team1 entities not decoded: %q", m.Teams[0].Name)
	}
	if m.Teams[1].Name != "A & B" {
		t.Errorf("team2 entities not decoded: %q", m.Teams[1].Name)
	}
	if m.Tournament.Name != "Test & Event" {
		t.Errorf("tournament entities not decoded: %q", m.Tournament.Name)
	}
}

func TestParseScoreTransitions(t *testing.T) {
	mkBlock := func(score string, extras string) string {
		return `<div class="match-info">` +
			`<span class="timer-object" data-timestamp="1775923200">x</span>` +
			`<div class="block-team"><a title="Team A">A</a></div>` +
			`<div class="block-team"><a title="Team B">B</a></div>` +
			`<span class="match-info-header-scoreholder-upper">` + score + `</span>` +
			extras +
			`<div class="match-info-tournament-name"><span>Event</span></div>` +
			`<a href="/rocketleague/Special:Stream/twitch/RocketStreetLive" title=""></a>` +
			`</div>`
	}

	// start is "1775923200" → 2026-04-11T16:00:00Z
	preStart := time.Date(2026, 4, 11, 15, 30, 0, 0, time.UTC)
	earlyLive := time.Date(2026, 4, 11, 16, 5, 0, 0, time.UTC)
	afterFinal := time.Date(2026, 4, 11, 23, 0, 0, 0, time.UTC)

	t.Run("vs before start → upcoming", func(t *testing.T) {
		ms := Parse(mkBlock("vs", ""), "rocketleague", preStart, 5, 90*time.Minute)
		if len(ms) != 1 || ms[0].Status != model.StatusUpcoming {
			t.Errorf("want upcoming, got %+v", statusOf(ms))
		}
	})

	t.Run("two scores early → live", func(t *testing.T) {
		// Embed a "1-0" score pair somewhere the scorePair regex can find it.
		ms := Parse(mkBlock("1", `<div class="match-info-header-scoreholder-lower">1-0</div>`), "rocketleague", earlyLive, 5, 90*time.Minute)
		if len(ms) != 1 || ms[0].Status != model.StatusLive {
			t.Errorf("want live, got %+v", statusOf(ms))
		}
		if ms[0].Teams[0].Score == nil || *ms[0].Teams[0].Score != 1 {
			t.Errorf("team1 score: %v", ms[0].Teams[0].Score)
		}
	})

	t.Run("bo5 final score triggers final", func(t *testing.T) {
		ms := Parse(mkBlock("3", `<div class="match-info-header-scoreholder-lower">3-2</div>`), "rocketleague", earlyLive, 5, 90*time.Minute)
		if len(ms) != 1 || ms[0].Status != model.StatusFinal {
			t.Errorf("want final (3>=ceil(5/2)+1=3), got %+v", statusOf(ms))
		}
	})

	t.Run("past end time → final", func(t *testing.T) {
		ms := Parse(mkBlock("1", `<div class="match-info-header-scoreholder-lower">1-2</div>`), "rocketleague", afterFinal, 5, 90*time.Minute)
		if len(ms) != 1 || ms[0].Status != model.StatusFinal {
			t.Errorf("want final (past end time), got %+v", statusOf(ms))
		}
	})
}

func statusOf(ms []model.Match) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Status.String()
	}
	return out
}

// --- Golden file helpers ---

type goldenMatch struct {
	Key        string                `json:"key"`
	Game       string                `json:"game"`
	StartTime  string                `json:"start_time"`
	Teams      [2]goldenTeam         `json:"teams"`
	Tournament goldenTournament      `json:"tournament"`
	Status     string                `json:"status"`
	BestOf     int                   `json:"best_of,omitempty"`
	Streams    []goldenStream        `json:"streams"`
}

type goldenTeam struct {
	Name  string `json:"name"`
	Score *int   `json:"score,omitempty"`
}

type goldenTournament struct {
	Name   string `json:"name"`
	Tier   string `json:"tier,omitempty"`
	Region string `json:"region,omitempty"`
}

type goldenStream struct {
	Platform string `json:"platform"`
	Channel  string `json:"channel"`
	URL      string `json:"url"`
}

func toGolden(ms []model.Match) []goldenMatch {
	out := make([]goldenMatch, len(ms))
	for i, m := range ms {
		g := goldenMatch{
			Key:       m.Key,
			Game:      m.Game,
			StartTime: m.StartTime.UTC().Format(time.RFC3339),
			Status:    m.Status.String(),
			BestOf:    m.BestOf,
			Tournament: goldenTournament{
				Name:   m.Tournament.Name,
				Tier:   m.Tournament.Tier,
				Region: m.Tournament.Region,
			},
		}
		for ti, t := range m.Teams {
			g.Teams[ti] = goldenTeam{Name: t.Name, Score: t.Score}
		}
		for _, s := range m.Streams {
			g.Streams = append(g.Streams, goldenStream{Platform: s.Platform, Channel: s.Channel, URL: s.URL})
		}
		out[i] = g
	}
	return out
}

func writeGolden(t *testing.T, path string, ms []model.Match) {
	t.Helper()
	data, err := json.MarshalIndent(toGolden(ms), "", "  ")
	if err != nil {
		t.Fatalf("marshal golden: %v", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write golden: %v", err)
	}
	t.Logf("wrote golden: %s (%d matches)", path, len(ms))
}

func readGolden(t *testing.T, path string) []goldenMatch {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (run with -update to regenerate)", err)
	}
	var out []goldenMatch
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("parse golden: %v", err)
	}
	return out
}

func compareMatches(t *testing.T, want []goldenMatch, gotMatches []model.Match) {
	t.Helper()
	got := toGolden(gotMatches)
	if len(got) != len(want) {
		t.Fatalf("match count: got %d, want %d", len(got), len(want))
	}
	// Compare as pretty-printed JSON for readable diffs.
	wantJSON, _ := json.MarshalIndent(want, "", "  ")
	gotJSON, _ := json.MarshalIndent(got, "", "  ")
	if string(wantJSON) != string(gotJSON) {
		t.Errorf("golden mismatch — run with -update if expected:\n--- want\n%s\n--- got\n%s",
			firstLines(string(wantJSON), 30), firstLines(string(gotJSON), 30))
	}
}

func firstLines(s string, n int) string {
	lines := strings.SplitN(s, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
		lines = append(lines, "...")
	}
	return strings.Join(lines, "\n")
}
