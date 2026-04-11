package main

import (
	"time"

	"github.com/jsabella/gridwatch/internal/model"
	"github.com/jsabella/gridwatch/internal/store"
)

// seedDemo loads a handful of fabricated matches so the UI can render
// immediately without hitting Liquipedia. Used by --demo mode for README
// screenshots and offline local development.
func seedDemo(s *store.Store) error {
	now := time.Now().UTC()
	mk := func(offset time.Duration, game, t1, t2, tour string, status model.Status) model.Match {
		start := now.Add(offset)
		m := model.Match{
			Key:        model.MatchKey("demo", game, start.Unix(), t1, t2, tour),
			Source:     "demo",
			Game:       game,
			StartTime:  start,
			Status:     status,
			StatusText: status.String(),
			Teams:      [2]model.Team{{Name: t1}, {Name: t2}},
			Tournament: model.Tournament{Name: tour, Tier: "S-Tier", Region: "EU"},
			BestOf:     5,
			Streams: []model.Stream{
				{Platform: "twitch", Channel: "RocketLeague", URL: "https://twitch.tv/RocketLeague"},
			},
		}
		return m
	}

	matches := []model.Match{
		mk(-20*time.Minute, "rocketleague", "G2 Esports", "Karmine Corp", "RLCS 2026 — World Championship", model.StatusLive),
		mk(-40*time.Minute, "leagueoflegends", "T1", "Gen.G", "LCK Spring Finals", model.StatusLive),
		mk(30*time.Minute, "counterstrike", "NAVI", "FaZe Clan", "IEM Katowice", model.StatusUpcoming),
		mk(90*time.Minute, "rocketleague", "Team Vitality", "FURIA", "RLCS 2026 — EU Major", model.StatusUpcoming),
		mk(3*time.Hour, "dota2", "Team Liquid", "OG", "The International", model.StatusUpcoming),
		mk(5*time.Hour, "valorant", "Sentinels", "100 Thieves", "VCT Americas", model.StatusUpcoming),
		mk(-2*time.Hour, "rocketleague", "Moist Esports", "BDS", "RLCS 2026 — NA Regional", model.StatusFinal),
	}
	// Fill final match with a score.
	three := 3
	one := 1
	matches[len(matches)-1].Teams[0].Score = &three
	matches[len(matches)-1].Teams[1].Score = &one

	// Group by game and merge so the store's per-game indexing is exercised.
	byGame := map[string][]model.Match{}
	for _, m := range matches {
		byGame[m.Game] = append(byGame[m.Game], m)
	}
	for game, gm := range byGame {
		s.Merge("demo", game, gm, now)
	}
	return nil
}
