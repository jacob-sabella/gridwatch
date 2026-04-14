package model

import "testing"

func TestMatchKeyStable(t *testing.T) {
	a := MatchKey("liquipedia", "rocketleague", 1775919600, "MCI", "GriddyGoose", "RLCS 2026")
	b := MatchKey("liquipedia", "rocketleague", 1775919600, "MCI", "GriddyGoose", "RLCS 2026")
	if a != b {
		t.Fatalf("keys differed for identical input: %q vs %q", a, b)
	}
	if len(a) != 2+12 { // "m_" + 12 hex chars
		t.Fatalf("unexpected key length: %q", a)
	}
}

func TestMatchKeyTeamOrderInsensitive(t *testing.T) {
	a := MatchKey("liquipedia", "rocketleague", 1775919600, "MCI", "GriddyGoose", "RLCS 2026")
	b := MatchKey("liquipedia", "rocketleague", 1775919600, "GriddyGoose", "MCI", "RLCS 2026")
	if a != b {
		t.Fatalf("team order should not affect key: %q vs %q", a, b)
	}
}

func TestMatchKeyCaseInsensitiveTeams(t *testing.T) {
	a := MatchKey("liquipedia", "rl", 100, "TEAM A", "Team B", "T")
	b := MatchKey("liquipedia", "rl", 100, "team a", "team b", "T")
	if a != b {
		t.Fatalf("team case should not affect key: %q vs %q", a, b)
	}
}

func TestMatchKeyDifferentiatesDistinctMatches(t *testing.T) {
	a := MatchKey("liquipedia", "rocketleague", 1775919600, "MCI", "GriddyGoose", "RLCS 2026")
	b := MatchKey("liquipedia", "rocketleague", 1775923200, "MCI", "GriddyGoose", "RLCS 2026")
	if a == b {
		t.Fatalf("different timestamps should yield different keys")
	}
	c := MatchKey("liquipedia", "rocketleague", 1775919600, "MCI", "GriddyGoose", "RLCS 2025")
	if a == c {
		t.Fatalf("different tournaments should yield different keys")
	}
	d := MatchKey("liquipedia", "leagueoflegends", 1775919600, "MCI", "GriddyGoose", "RLCS 2026")
	if a == d {
		t.Fatalf("different games should yield different keys")
	}
}
