// Package config loads and validates gridwatch configuration from a YAML
// file, with environment-variable overlays for secrets and deployment
// tweaks.
//
// The loader applies defaults first, then the YAML, then the env overlay,
// then validation. Validation failures refuse to start the process —
// specifically, a missing `contact` is a hard error because it's required
// by Liquipedia's Terms of Use.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jsabella/gridwatch/internal/model"
	"gopkg.in/yaml.v3"
)

// Config is the full configuration shape.
type Config struct {
	Contact         string        `yaml:"contact"`
	Bind            string        `yaml:"bind"`
	BaseURL         string        `yaml:"base_url"`
	DefaultTimezone string        `yaml:"default_timezone"`
	DataDir         string        `yaml:"data_dir"`
	Snapshot        Snapshot      `yaml:"snapshot"`
	Poll            Poll          `yaml:"poll"`
	Games           []GameConfig  `yaml:"games"`
	View            View          `yaml:"view"`
	Notifications   Notifications `yaml:"notifications"`
	Metrics         bool          `yaml:"metrics"`
	LogLevel        string        `yaml:"log_level"`
}

// GameConfig is either a bare slug ("rocketleague") or a full object with
// overrides. YAML unmarshaling handles both forms.
type GameConfig struct {
	Slug          string        `yaml:"slug"`
	Display       string        `yaml:"display,omitempty"`
	Color         string        `yaml:"color,omitempty"`
	DefaultBestOf int           `yaml:"default_best_of,omitempty"`
	MatchDuration time.Duration `yaml:"match_duration,omitempty"`
}

// UnmarshalYAML supports both `- rocketleague` and `- slug: rocketleague`.
func (g *GameConfig) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		g.Slug = node.Value
		return nil
	}
	type raw GameConfig
	var r raw
	if err := node.Decode(&r); err != nil {
		return err
	}
	*g = GameConfig(r)
	return nil
}

// Merge returns the GameConfig with fields from the default filled in
// where this one is zero.
func (g GameConfig) Merge(def model.Game) model.Game {
	out := def
	if g.Display != "" {
		out.Display = g.Display
	}
	if g.Color != "" {
		out.Color = g.Color
	}
	if g.DefaultBestOf != 0 {
		out.DefaultBestOf = g.DefaultBestOf
	}
	if g.MatchDuration != 0 {
		out.MatchDuration = g.MatchDuration
	}
	return out
}

// Snapshot controls on-disk caching of store state.
type Snapshot struct {
	Enabled  bool          `yaml:"enabled"`
	Interval time.Duration `yaml:"interval"`
}

// Poll controls upstream polling rates.
type Poll struct {
	GlobalRPS          float64       `yaml:"global_rps"`
	LiquipediaInterval time.Duration `yaml:"liquipedia_interval"`
	CacheTTL           time.Duration `yaml:"cache_ttl"`
	BackoffTTL         time.Duration `yaml:"backoff_ttl"`
	Jitter             time.Duration `yaml:"jitter"`
}

// View controls the time window shown in the grid.
type View struct {
	WindowPast   time.Duration `yaml:"window_past"`
	WindowFuture time.Duration `yaml:"window_future"`
	Slot         time.Duration `yaml:"slot"`
	Theme        string        `yaml:"theme"` // "auto" | "dark" | "light"
}

// Notifications controls the opt-in notifier subsystem.
type Notifications struct {
	Enabled           bool                 `yaml:"enabled"`
	DedupeWindowHours int                  `yaml:"dedupe_window_hours"`
	Rules             []NotificationRule   `yaml:"rules"`
	Sinks             []NotificationSink   `yaml:"sinks"`
}

// NotificationRule picks which matches produce which notifications.
type NotificationRule struct {
	Games   []string `yaml:"games"`
	Stages  []string `yaml:"stages"`
	MinTier string   `yaml:"min_tier"`
	Regions []string `yaml:"regions"`
}

// NotificationSink is a single delivery backend. Which fields are used
// depends on Kind.
type NotificationSink struct {
	Kind        string            `yaml:"kind"` // "ntfy" | "webhook"
	URL         string            `yaml:"url"`
	Topic       string            `yaml:"topic,omitempty"`
	User        string            `yaml:"user,omitempty"`
	Password    string            `yaml:"password,omitempty"`
	Method      string            `yaml:"method,omitempty"`
	Headers     map[string]string `yaml:"headers,omitempty"`
	PriorityMap map[string]int    `yaml:"priority_map,omitempty"`
}

// Defaults returns a Config with sensible defaults applied but no games
// registered. Load() fills in games from the YAML overlay.
func Defaults() Config {
	return Config{
		Bind:            "0.0.0.0:8080",
		BaseURL:         "",
		DefaultTimezone: "America/Chicago",
		DataDir:         "/data",
		LogLevel:        "info",
		Snapshot: Snapshot{
			Enabled:  true,
			Interval: 5 * time.Minute,
		},
		Poll: Poll{
			GlobalRPS:          0.2,
			LiquipediaInterval: 120 * time.Second,
			CacheTTL:           90 * time.Second,
			BackoffTTL:         10 * time.Minute,
			Jitter:             20 * time.Second,
		},
		View: View{
			WindowPast:   2 * time.Hour,
			WindowFuture: 24 * time.Hour,
			Slot:         30 * time.Minute,
			Theme:        "auto",
		},
		Notifications: Notifications{
			Enabled:           false,
			DedupeWindowHours: 48,
		},
		Metrics: false,
	}
}

// Load reads a YAML file, applies defaults, merges env vars, and validates.
// If path is empty, only defaults + env are used.
func Load(path string) (*Config, error) {
	cfg := Defaults()

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read config %s: %w", path, err)
		}
		// Expand ${VAR} before parsing so env-referenced secrets land in
		// the right types.
		expanded := expandEnv(string(data))
		if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
			return nil, fmt.Errorf("parse config %s: %w", path, err)
		}
	}

	applyEnvOverlay(&cfg)

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	// Resolve DataDir to absolute so snapshot paths don't depend on cwd.
	if !filepath.IsAbs(cfg.DataDir) {
		abs, err := filepath.Abs(cfg.DataDir)
		if err == nil {
			cfg.DataDir = abs
		}
	}

	return &cfg, nil
}

// ResolvedGames merges per-user game configs with the shipped defaults,
// producing the list the poller and UI use. If the user lists a game
// slug that gridwatch doesn't have metadata for, the Game is synthesized
// with reasonable fallbacks so new wikis "just work" without a release.
func (c *Config) ResolvedGames() []model.Game {
	defaults := map[string]model.Game{}
	for _, g := range model.Defaults() {
		defaults[g.Slug] = g
	}
	out := make([]model.Game, 0, len(c.Games))
	for _, gc := range c.Games {
		def, ok := defaults[gc.Slug]
		if !ok {
			def = model.Game{
				Slug:          gc.Slug,
				Display:       titleize(gc.Slug),
				Color:         "#888888",
				DefaultBestOf: 3,
				MatchDuration: 90 * time.Minute,
			}
		}
		out = append(out, gc.Merge(def))
	}
	return out
}

func titleize(slug string) string {
	parts := strings.Split(slug, "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

// expandEnv replaces ${VAR} with os.Getenv("VAR"). Unlike os.ExpandEnv,
// it leaves $X alone so dollar signs in passwords survive.
var envExpandRE = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

func expandEnv(s string) string {
	return envExpandRE.ReplaceAllStringFunc(s, func(m string) string {
		name := m[2 : len(m)-1]
		return os.Getenv(name)
	})
}

// applyEnvOverlay reads GRIDWATCH_* variables and applies them on top of
// the YAML values. Only a handful of fields are supported — the ones
// users commonly tweak per-environment.
func applyEnvOverlay(cfg *Config) {
	if v := os.Getenv("GRIDWATCH_CONTACT"); v != "" {
		cfg.Contact = v
	}
	if v := os.Getenv("GRIDWATCH_BIND"); v != "" {
		cfg.Bind = v
	}
	if v := os.Getenv("GRIDWATCH_BASE_URL"); v != "" {
		cfg.BaseURL = v
	}
	if v := os.Getenv("GRIDWATCH_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	if v := os.Getenv("GRIDWATCH_DEFAULT_TIMEZONE"); v != "" {
		cfg.DefaultTimezone = v
	}
	if v := os.Getenv("GRIDWATCH_LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
	if v := os.Getenv("GRIDWATCH_METRICS"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Metrics = b
		}
	}
}
