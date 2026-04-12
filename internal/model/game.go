package model

import "time"

// Game is the per-title metadata used by config and UI.
// Defaults ship with gridwatch; users can override colors/durations.
type Game struct {
	Slug          string        `yaml:"slug" json:"slug"`
	Display       string        `yaml:"display" json:"display"`
	Color         string        `yaml:"color" json:"color"`
	DefaultBestOf int           `yaml:"default_best_of" json:"default_best_of"`
	MatchDuration time.Duration `yaml:"match_duration" json:"match_duration"`
}

// Defaults returns the shipped set of supported games with sensible
// per-title defaults. Users can override any field via config.
func Defaults() []Game {
	return []Game{
		{Slug: "rocketleague", Display: "Rocket League", Color: "#1f6bff", DefaultBestOf: 5, MatchDuration: 90 * time.Minute},
		{Slug: "leagueoflegends", Display: "League of Legends", Color: "#c89b3c", DefaultBestOf: 5, MatchDuration: 60 * time.Minute},
		{Slug: "counterstrike", Display: "Counter-Strike 2", Color: "#f59e0b", DefaultBestOf: 3, MatchDuration: 90 * time.Minute},
		{Slug: "dota2", Display: "Dota 2", Color: "#a03c3c", DefaultBestOf: 3, MatchDuration: 75 * time.Minute},
		{Slug: "valorant", Display: "Valorant", Color: "#fd4556", DefaultBestOf: 3, MatchDuration: 90 * time.Minute},
		{Slug: "starcraft2", Display: "StarCraft II", Color: "#7d3cff", DefaultBestOf: 5, MatchDuration: 60 * time.Minute},
		{Slug: "overwatch", Display: "Overwatch", Color: "#f99e1a", DefaultBestOf: 5, MatchDuration: 45 * time.Minute},
		{Slug: "rematch", Display: "Rematch", Color: "#22c55e", DefaultBestOf: 3, MatchDuration: 60 * time.Minute},
		{Slug: "osu", Display: "osu!", Color: "#ff66aa", DefaultBestOf: 5, MatchDuration: 60 * time.Minute},
		{Slug: "smash", Display: "Smash Bros.", Color: "#e63946", DefaultBestOf: 5, MatchDuration: 45 * time.Minute},
		{Slug: "fighters", Display: "Fighting Games", Color: "#9d4edd", DefaultBestOf: 5, MatchDuration: 45 * time.Minute},
		{Slug: "thefinals", Display: "The Finals", Color: "#00d4ff", DefaultBestOf: 3, MatchDuration: 60 * time.Minute},
	}
}
