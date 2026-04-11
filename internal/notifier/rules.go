package notifier

import (
	"github.com/jsabella/gridwatch/internal/config"
	"github.com/jsabella/gridwatch/internal/model"
)

// RuleEngine evaluates whether a (match, stage) tuple should fire given
// a set of config rules. Pure function — no state, no I/O.
type RuleEngine struct {
	rules []config.NotificationRule
}

// NewRuleEngine constructs a rule engine. If rules is empty, all matches
// fire all stages ("default-on"), which is the sensible UX for a brand-
// new install.
func NewRuleEngine(rules []config.NotificationRule) *RuleEngine {
	return &RuleEngine{rules: rules}
}

// Matches returns true if at least one rule allows the match+stage combo,
// or if there are no rules configured (default-on).
func (e *RuleEngine) Matches(m model.Match, stage Stage) bool {
	if len(e.rules) == 0 {
		return true
	}
	for _, r := range e.rules {
		if ruleMatches(r, m, stage) {
			return true
		}
	}
	return false
}

func ruleMatches(r config.NotificationRule, m model.Match, stage Stage) bool {
	if len(r.Games) > 0 && !containsStr(r.Games, m.Game) {
		return false
	}
	if len(r.Stages) > 0 && !containsStr(r.Stages, string(stage)) {
		return false
	}
	if len(r.Regions) > 0 && !containsStr(r.Regions, m.Tournament.Region) {
		return false
	}
	if r.MinTier != "" && !tierAtLeast(m.Tournament.Tier, r.MinTier) {
		return false
	}
	return true
}

// tierAtLeast compares Liquipedia tier labels lexicographically. S-Tier
// is the top, D-Tier is the bottom. We map to a numeric rank and then
// compare. Unknown tiers pass the filter (can't second-guess the user).
func tierAtLeast(have, min string) bool {
	ranks := map[string]int{
		"S-Tier": 5,
		"A-Tier": 4,
		"B-Tier": 3,
		"C-Tier": 2,
		"D-Tier": 1,
	}
	if have == "" {
		return true
	}
	h, ok := ranks[have]
	if !ok {
		return true
	}
	needed, ok := ranks[min]
	if !ok {
		return true
	}
	return h >= needed
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
