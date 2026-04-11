package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jsabella/gridwatch/internal/model"
	"github.com/jsabella/gridwatch/internal/store"
	"github.com/jsabella/gridwatch/internal/timeutil"
)

// registerRoutes wires every HTTP path this server exposes. Stdlib
// ServeMux with Go 1.22 method patterns, zero framework dependency.
func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /partial/grid", s.handleGridPartial)
	mux.HandleFunc("GET /partial/cards", s.handleCardsPartial)
	mux.HandleFunc("GET /partial/filters", s.handleFiltersPartial)
	mux.HandleFunc("GET /events", s.handleSSE)
	mux.HandleFunc("GET /api/v1/matches", s.handleAPIMatchesJSON)
	mux.HandleFunc("GET /api/v1/matches.ics", s.handleAPIMatchesICS)
	mux.HandleFunc("GET /api/v1/matches.xml", s.handleAPIMatchesXMLTV)
	mux.HandleFunc("GET /api/v1/games", s.handleAPIGames)
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)
	mux.Handle("GET /static/", http.StripPrefix("/static/", s.staticHandler()))
	if s.cfg.Metrics {
		mux.HandleFunc("GET /metrics", s.handleMetrics)
	}
}

// ----- static -----

func (s *Server) staticHandler() http.Handler {
	fs := http.FS(s.templ.Static())
	h := http.FileServer(fs)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Long cache: assets are fingerprinted by path (version in binary)
		// and change only on release. Safe to cache aggressively.
		w.Header().Set("Cache-Control", "public, max-age=3600")
		h.ServeHTTP(w, r)
	})
}

// ----- index + partials -----

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	data := s.viewData(r)
	if isHTMX(r) {
		s.render(w, "partials/grid.html", data)
		return
	}
	s.render(w, "index.html", data)
}

func (s *Server) handleGridPartial(w http.ResponseWriter, r *http.Request) {
	s.render(w, "partials/grid.html", s.viewData(r))
}

func (s *Server) handleCardsPartial(w http.ResponseWriter, r *http.Request) {
	s.render(w, "partials/cards.html", s.viewData(r))
}

func (s *Server) handleFiltersPartial(w http.ResponseWriter, r *http.Request) {
	s.render(w, "partials/filters.html", s.viewData(r))
}

// ----- JSON API -----

func (s *Server) handleAPIMatchesJSON(w http.ResponseWriter, r *http.Request) {
	q := s.queryFromRequest(r)
	matches := s.store.Query(q)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	payload := struct {
		Revision int64         `json:"revision"`
		Count    int           `json:"count"`
		Matches  []model.Match `json:"matches"`
	}{
		Revision: s.store.Revision(),
		Count:    len(matches),
		Matches:  matches,
	}
	_ = json.NewEncoder(w).Encode(payload)
}

func (s *Server) handleAPIGames(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.games)
}

// ----- iCal (RFC 5545) -----

func (s *Server) handleAPIMatchesICS(w http.ResponseWriter, r *http.Request) {
	// Show a wide window for calendar subscription so users see upcoming
	// matches even if their client doesn't refresh frequently.
	now := time.Now().UTC()
	q := store.FilterQuery{
		HasStream: true,
		Window: store.TimeWindow{
			Reference: now,
			Past:      12 * time.Hour,
			Future:    14 * 24 * time.Hour,
		},
	}
	matches := s.store.Query(q)

	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `inline; filename="gridwatch.ics"`)
	w.Header().Set("Cache-Control", "public, max-age=300")

	const (
		layout = "20060102T150405Z"
	)
	fmt.Fprint(w, "BEGIN:VCALENDAR\r\n")
	fmt.Fprint(w, "VERSION:2.0\r\n")
	fmt.Fprint(w, "PRODID:-//gridwatch//EN\r\n")
	fmt.Fprint(w, "CALSCALE:GREGORIAN\r\n")
	fmt.Fprint(w, "X-WR-CALNAME:Esports (gridwatch)\r\n")
	fmt.Fprint(w, "X-WR-TIMEZONE:UTC\r\n")

	for _, m := range matches {
		end := m.StartTime.Add(gameDuration(s.games, m.Game))
		summary := fmt.Sprintf("%s vs %s — %s", m.Teams[0].Name, m.Teams[1].Name, m.Tournament.Name)
		description := fmt.Sprintf("Game: %s\\nTournament: %s\\nStream: %s",
			m.Game, m.Tournament.Name, m.PrimaryStream())

		fmt.Fprint(w, "BEGIN:VEVENT\r\n")
		fmt.Fprintf(w, "UID:%s@gridwatch\r\n", m.Key)
		fmt.Fprintf(w, "DTSTAMP:%s\r\n", now.Format(layout))
		fmt.Fprintf(w, "DTSTART:%s\r\n", m.StartTime.UTC().Format(layout))
		fmt.Fprintf(w, "DTEND:%s\r\n", end.UTC().Format(layout))
		fmt.Fprintf(w, "SUMMARY:%s\r\n", icsEscape(summary))
		fmt.Fprintf(w, "DESCRIPTION:%s\r\n", icsEscape(description))
		if u := m.PrimaryStream(); u != "" {
			fmt.Fprintf(w, "URL:%s\r\n", u)
		}
		fmt.Fprint(w, "END:VEVENT\r\n")
	}
	fmt.Fprint(w, "END:VCALENDAR\r\n")
}

func icsEscape(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, ";", "\\;")
	s = strings.ReplaceAll(s, ",", "\\,")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}

// ----- XMLTV (for Plex Live TV / xTeVe / Jellyfin Live TV) -----

func (s *Server) handleAPIMatchesXMLTV(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	q := store.FilterQuery{
		HasStream: true,
		Window: store.TimeWindow{
			Reference: now,
			Past:      12 * time.Hour,
			Future:    14 * 24 * time.Hour,
		},
	}
	matches := s.store.Query(q)

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")

	fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>`+"\n")
	fmt.Fprint(w, `<tv generator-info-name="gridwatch">`+"\n")

	// Emit one channel per game (so the grid shows rows labeled by game).
	for _, g := range s.games {
		fmt.Fprintf(w, `  <channel id="gridwatch-%s"><display-name>%s</display-name></channel>`+"\n",
			xmlEscape(g.Slug), xmlEscape(g.Display))
	}

	const layout = "20060102150405 -0000"
	for _, m := range matches {
		end := m.StartTime.Add(gameDuration(s.games, m.Game))
		title := fmt.Sprintf("%s vs %s", m.Teams[0].Name, m.Teams[1].Name)
		fmt.Fprintf(w, `  <programme start="%s" stop="%s" channel="gridwatch-%s">`+"\n",
			m.StartTime.UTC().Format(layout), end.UTC().Format(layout), xmlEscape(m.Game))
		fmt.Fprintf(w, `    <title lang="en">%s</title>`+"\n", xmlEscape(title))
		fmt.Fprintf(w, `    <sub-title lang="en">%s</sub-title>`+"\n", xmlEscape(m.Tournament.Name))
		fmt.Fprintf(w, `    <category lang="en">Sports</category>`+"\n")
		fmt.Fprintf(w, `    <category lang="en">Esports</category>`+"\n")
		if m.Status == model.StatusLive {
			fmt.Fprintf(w, `    <live />`+"\n")
		}
		if u := m.PrimaryStream(); u != "" {
			fmt.Fprintf(w, `    <url>%s</url>`+"\n", xmlEscape(u))
		}
		fmt.Fprint(w, `  </programme>`+"\n")
	}
	fmt.Fprint(w, `</tv>`+"\n")
}

func xmlEscape(s string) string {
	var out strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			out.WriteString("&amp;")
		case '<':
			out.WriteString("&lt;")
		case '>':
			out.WriteString("&gt;")
		case '"':
			out.WriteString("&quot;")
		case '\'':
			out.WriteString("&apos;")
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}

// ----- health -----

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	last := s.store.LastSuccessfulPoll()
	if last.IsZero() {
		// First poll hasn't landed yet.
		http.Error(w, "warming up", http.StatusServiceUnavailable)
		return
	}
	// Freshness SLA: 2x the poll interval.
	if time.Since(last) > 2*s.cfg.Poll.LiquipediaInterval {
		http.Error(w, "stale", http.StatusServiceUnavailable)
		return
	}
	fmt.Fprint(w, "ok")
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if !s.store.Ready() {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	fmt.Fprint(w, "ready")
}

// ----- metrics (optional) -----

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	all := s.store.Query(store.FilterQuery{})
	countByStatus := map[string]int{}
	countByGame := map[string]int{}
	for _, m := range all {
		countByStatus[m.Status.String()]++
		countByGame[m.Game]++
	}
	fmt.Fprintf(w, "# HELP gridwatch_matches_total Matches currently tracked\n")
	fmt.Fprintf(w, "# TYPE gridwatch_matches_total gauge\n")
	for status, n := range countByStatus {
		fmt.Fprintf(w, "gridwatch_matches_total{status=%q} %d\n", status, n)
	}
	fmt.Fprintf(w, "# HELP gridwatch_matches_by_game Matches per game\n")
	fmt.Fprintf(w, "# TYPE gridwatch_matches_by_game gauge\n")
	for game, n := range countByGame {
		fmt.Fprintf(w, "gridwatch_matches_by_game{game=%q} %d\n", game, n)
	}
	last := s.store.LastSuccessfulPoll()
	if !last.IsZero() {
		fmt.Fprintf(w, "# HELP gridwatch_last_successful_poll_timestamp_seconds UNIX timestamp of most recent successful upstream poll\n")
		fmt.Fprintf(w, "# TYPE gridwatch_last_successful_poll_timestamp_seconds gauge\n")
		fmt.Fprintf(w, "gridwatch_last_successful_poll_timestamp_seconds %d\n", last.Unix())
	}
	fmt.Fprintf(w, "# HELP gridwatch_store_revision Current store revision (monotonic)\n")
	fmt.Fprintf(w, "# TYPE gridwatch_store_revision counter\n")
	fmt.Fprintf(w, "gridwatch_store_revision %d\n", s.store.Revision())
}

// ----- filter parsing -----

func (s *Server) queryFromRequest(r *http.Request) store.FilterQuery {
	qs := r.URL.Query()
	q := store.FilterQuery{}

	if games := qs["game"]; len(games) > 0 {
		q.Games = games
	}
	if regions := qs["region"]; len(regions) > 0 {
		q.Regions = regions
	}
	if tiers := qs["tier"]; len(tiers) > 0 {
		q.Tiers = tiers
	}
	// has_stream=true (default). Explicitly set has_stream=0 to include
	// stream-less matches.
	if qs.Get("has_stream") == "0" || qs.Get("has_stream") == "false" {
		q.HasStream = false
	} else {
		q.HasStream = true
	}

	// Default time window from view config.
	now := time.Now().UTC()
	q.Window = store.TimeWindow{
		Reference: now,
		Past:      s.cfg.View.WindowPast,
		Future:    s.cfg.View.WindowFuture,
	}
	return q
}

// gameDuration returns the configured match duration for a game, or a
// 90-minute fallback.
func gameDuration(games []model.Game, slug string) time.Duration {
	for _, g := range games {
		if g.Slug == slug {
			return g.MatchDuration
		}
	}
	return 90 * time.Minute
}

// viewData builds the template data structure used by index and partial
// handlers. Kept small because the templates do most of the work.
func (s *Server) viewData(r *http.Request) map[string]any {
	q := s.queryFromRequest(r)
	matches := s.store.Query(q)

	tz := timeutil.LoadLocation(s.cfg.DefaultTimezone)
	if c, err := r.Cookie("gw_tz"); err == nil && c.Value != "" {
		if loc, err := time.LoadLocation(c.Value); err == nil {
			tz = loc
		}
	}
	now := time.Now().In(tz)

	// Partition matches.
	var live, upcoming, recent []model.Match
	for _, m := range matches {
		switch m.Status {
		case model.StatusLive:
			live = append(live, m)
		case model.StatusFinal:
			recent = append(recent, m)
		default:
			upcoming = append(upcoming, m)
		}
	}

	// Build slot list for the EPG grid (timeline columns).
	windowStart := timeutil.FloorSlot(now.Add(-s.cfg.View.WindowPast), s.cfg.View.Slot)
	windowEnd := now.Add(s.cfg.View.WindowFuture)
	slotCount := timeutil.SlotCount(windowStart, windowEnd, s.cfg.View.Slot)
	if slotCount < 1 {
		slotCount = 1
	}

	type slotInfo struct {
		Label string
		Start time.Time
	}
	slots := make([]slotInfo, 0, slotCount)
	for i := 0; i < slotCount; i++ {
		t := windowStart.Add(time.Duration(i) * s.cfg.View.Slot)
		// 12h label — show ":00 AM/PM" on the hour, minutes-only between.
		// Produces: "3 PM", "3:30", "4 PM", "4:30", etc. Compact + readable.
		var label string
		if t.Minute() == 0 {
			label = t.Format("3 PM")
		} else {
			label = t.Format(":04")
		}
		slots = append(slots, slotInfo{Label: label, Start: t})
	}

	nowIdx := timeutil.SlotIndex(now, windowStart, s.cfg.View.Slot, slotCount)

	return map[string]any{
		"BaseURL":      s.baseURL,
		"Version":      s.startAt,
		"Games":        s.games,
		"Live":         live,
		"Upcoming":     upcoming,
		"Recent":       recent,
		"AllMatches":   matches,
		"Slots":        slots,
		"SlotCount":    slotCount,
		"NowSlotIndex": nowIdx,
		"WindowStart":  windowStart,
		// Hand the window bounds to the client so JS can advance the
		// cursor and the header clock without a server round-trip.
		"WindowStartUnixMs": windowStart.UTC().UnixMilli(),
		"SlotMs":            s.cfg.View.Slot.Milliseconds(),
		"Now":               now,
		"Timezone":          tz.String(),
		"Theme":             s.cfg.View.Theme,
		"Filters":           q,
	}
}
