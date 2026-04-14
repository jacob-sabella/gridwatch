package config

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
)

// Validate checks that the config is safe to run against.
// Any error here is fatal at startup.
func (c *Config) Validate() error {
	// Contact is REQUIRED for Liquipedia ToU compliance. The User-Agent
	// string must carry contact info. Without it, the source would hit
	// the 406 / blocked path. We refuse to start.
	contact := strings.TrimSpace(c.Contact)
	if contact == "" {
		return errors.New("config: 'contact' is required (Liquipedia ToU: User-Agent must include contact info). Set it in your YAML or via GRIDWATCH_CONTACT")
	}
	if strings.ContainsAny(contact, "\r\n") {
		return errors.New("config: 'contact' must not contain newlines")
	}
	if !looksLikeContact(contact) {
		return fmt.Errorf("config: 'contact' doesn't look like a usable address (email or URL): %q", contact)
	}

	if len(c.Games) == 0 {
		return errors.New("config: 'games' must list at least one slug (e.g., rocketleague)")
	}
	seen := make(map[string]struct{}, len(c.Games))
	for _, g := range c.Games {
		if g.Slug == "" {
			return errors.New("config: a game entry has no slug")
		}
		if _, dup := seen[g.Slug]; dup {
			return fmt.Errorf("config: duplicate game slug %q", g.Slug)
		}
		seen[g.Slug] = struct{}{}
	}

	if c.Bind == "" {
		return errors.New("config: 'bind' must not be empty")
	}
	if c.Poll.GlobalRPS <= 0 {
		return errors.New("config: poll.global_rps must be > 0")
	}
	// Hard ToU ceiling: Liquipedia caps action=parse at 1 request per
	// 30 seconds TOTAL (0.033 RPS). We hard-reject anything at or
	// above the ceiling so there's no way to configure into a ban.
	// https://liquipedia.net/api-terms-of-use
	if c.Poll.GlobalRPS > 0.033 {
		return fmt.Errorf("config: poll.global_rps must be ≤ 0.033 (1 req / 30s, Liquipedia parse API ToU; default 0.0166 = 1 req / 60s is safer), got %v", c.Poll.GlobalRPS)
	}
	if c.Poll.LiquipediaInterval < 300*time.Second {
		return fmt.Errorf("config: poll.liquipedia_interval must be ≥ 300s (Liquipedia requests caching between calls; anything faster risks a temp IP ban). default 600s is safer. got %s", c.Poll.LiquipediaInterval)
	}
	if c.Poll.CacheTTL <= 0 {
		return errors.New("config: poll.cache_ttl must be > 0")
	}
	if c.Poll.CacheTTL < 30*time.Second {
		return fmt.Errorf("config: poll.cache_ttl must be ≥ 30s (matches Liquipedia's parse-endpoint minimum), got %s", c.Poll.CacheTTL)
	}
	if _, err := time.LoadLocation(c.DefaultTimezone); err != nil {
		return fmt.Errorf("config: invalid default_timezone %q: %w", c.DefaultTimezone, err)
	}

	switch c.View.Theme {
	case "", "auto", "dark", "light":
	default:
		return fmt.Errorf("config: view.theme must be auto|dark|light, got %q", c.View.Theme)
	}

	// Basic notification sanity — we only validate structure, not
	// connectivity, so misconfig doesn't crash at startup.
	for i, sink := range c.Notifications.Sinks {
		switch sink.Kind {
		case "ntfy":
			if sink.URL == "" || sink.Topic == "" {
				return fmt.Errorf("config: notifications.sinks[%d] (ntfy) requires url and topic", i)
			}
		case "webhook":
			if sink.URL == "" {
				return fmt.Errorf("config: notifications.sinks[%d] (webhook) requires url", i)
			}
		case "":
			return fmt.Errorf("config: notifications.sinks[%d] missing kind", i)
		default:
			return fmt.Errorf("config: notifications.sinks[%d] unknown kind %q", i, sink.Kind)
		}
	}

	return nil
}

// looksLikeContact accepts either a parseable email address or a URL-ish
// string (http/https prefix). Liquipedia wants something they can ping
// if we become a nuisance, and both forms satisfy that.
func looksLikeContact(s string) bool {
	if _, err := mail.ParseAddress(s); err == nil {
		return true
	}
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return true
	}
	return false
}
