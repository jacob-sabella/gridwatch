package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMinimalConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "g.yaml")
	content := `
contact: "me@example.com"
games:
  - rocketleague
  - leagueoflegends
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Contact != "me@example.com" {
		t.Errorf("contact: %q", cfg.Contact)
	}
	if len(cfg.Games) != 2 {
		t.Errorf("games: %d", len(cfg.Games))
	}
	if cfg.Games[0].Slug != "rocketleague" {
		t.Errorf("slug: %q", cfg.Games[0].Slug)
	}
	if cfg.Bind != "0.0.0.0:8080" {
		t.Errorf("bind default: %q", cfg.Bind)
	}
}

func TestLoadFullGameObject(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "g.yaml")
	content := `
contact: "me@example.com"
games:
  - slug: rocketleague
    color: "#ff0000"
    default_best_of: 7
    match_duration: 120m
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	games := cfg.ResolvedGames()
	if games[0].Color != "#ff0000" {
		t.Errorf("color override: %q", games[0].Color)
	}
	if games[0].DefaultBestOf != 7 {
		t.Errorf("best_of override: %d", games[0].DefaultBestOf)
	}
	if games[0].Display != "Rocket League" {
		t.Errorf("display inherited: %q", games[0].Display)
	}
}

func TestRejectsMissingContact(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "g.yaml")
	content := `
games:
  - rocketleague
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	// Clear any env var that might satisfy the check.
	t.Setenv("GRIDWATCH_CONTACT", "")
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing contact")
	}
	if !strings.Contains(err.Error(), "contact") {
		t.Errorf("error should mention contact: %v", err)
	}
}

func TestRejectsFastPollInterval(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "g.yaml")
	content := `
contact: "me@example.com"
games:
  - rocketleague
poll:
  liquipedia_interval: 60s
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for too-fast poll interval")
	}
	if !strings.Contains(err.Error(), "300s") {
		t.Errorf("error should mention 300s floor: %v", err)
	}
}

func TestRejectsTooHighGlobalRPS(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "g.yaml")
	content := `
contact: "me@example.com"
games:
  - rocketleague
poll:
  global_rps: 0.5
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for too-high global_rps")
	}
	if !strings.Contains(err.Error(), "0.033") {
		t.Errorf("error should mention 0.033 ceiling: %v", err)
	}
}

func TestEnvOverlayWins(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "g.yaml")
	content := `
contact: "base@example.com"
games:
  - rocketleague
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRIDWATCH_CONTACT", "override@example.com")
	t.Setenv("GRIDWATCH_BIND", "127.0.0.1:9999")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Contact != "override@example.com" {
		t.Errorf("contact overlay: %q", cfg.Contact)
	}
	if cfg.Bind != "127.0.0.1:9999" {
		t.Errorf("bind overlay: %q", cfg.Bind)
	}
}

func TestEnvInterpolationInYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "g.yaml")
	content := `
contact: "me@example.com"
games:
  - rocketleague
notifications:
  enabled: true
  sinks:
    - kind: ntfy
      url: http://ntfy.lan
      topic: alerts
      user: jacob
      password: "${TEST_NTFY_PASS}"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_NTFY_PASS", "super-secret")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Notifications.Sinks[0].Password != "super-secret" {
		t.Errorf("password interpolation failed: %q", cfg.Notifications.Sinks[0].Password)
	}
}

func TestRejectsUnknownTheme(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "g.yaml")
	content := `
contact: "me@example.com"
games:
  - rocketleague
view:
  theme: "rainbow"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "theme") {
		t.Errorf("expected theme error, got %v", err)
	}
}
