// Package liquipedia implements the Source interface against the
// Liquipedia MediaWiki parse API. The parser is the load-bearing logic,
// ported from rl-esports-tracker's PARSE_JS with golden-file tests so
// Liquipedia HTML drift fails loud.
package liquipedia

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jsabella/gridwatch/internal/model"
)

// ParseAPIResponse is the top-level shape returned by the Liquipedia
// parse API (action=parse&prop=text&format=json).
type ParseAPIResponse struct {
	Parse struct {
		Title  string `json:"title"`
		PageID int    `json:"pageid"`
		Text   struct {
			Star string `json:"*"`
		} `json:"text"`
	} `json:"parse"`
}

// DecodeResponse pulls the HTML blob out of the parse API response.
func DecodeResponse(body []byte) (string, error) {
	var r ParseAPIResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("decode parse api: %w", err)
	}
	if r.Parse.Text.Star == "" {
		return "", fmt.Errorf("parse api returned empty text")
	}
	return r.Parse.Text.Star, nil
}

// Regexes. Kept as package-level for compile-once semantics.
var (
	reTimestamp  = regexp.MustCompile(`data-timestamp="(\d+)"`)
	reTeam       = regexp.MustCompile(`(?s)class="block-team[^"]*".*?title="([^"]+)"`)
	reScoreSpan  = regexp.MustCompile(`match-info-header-scoreholder-upper"?[^>]*>([^<]*)</span>`)
	reTournament = regexp.MustCompile(`(?s)class="match-info-tournament-name"[^>]*>.*?<span>([^<]+)</span>`)
	reStream     = regexp.MustCompile(`Special:Stream/(twitch|youtube)/([^"]+)"`)
	// reScorePair catches "3-2" or "3:2" in any text context; used as a
	// fallback score parser when the upper scoreholder holds a digit
	// rather than "vs".
	reScorePair = regexp.MustCompile(`(\d+)\s*[:\-]\s*(\d+)`)
	// reTier tries to find a liquipediatier marker. Best-effort; not all
	// game wikis expose this in the match block.
	reTier = regexp.MustCompile(`data-filter-group="filterbuttons-liquipediatier"[^>]*data-filter-on="(\d+)"`)
	// reRegion looks for a region filter badge.
	reRegion = regexp.MustCompile(`data-filter-group="filterbuttons-region"[^>]*data-filter-on="(\w+)"`)
)

// Parse extracts matches from Liquipedia HTML.
//
// The input is the raw HTML string returned by the parse API (i.e., the
// content of parse.text["*"]). Returns a slice of model.Match with
// Source unset — the caller fills it in so the parser is reusable for
// non-Liquipedia sources that might mirror the HTML format.
//
// Matches without a timestamp, without 2 teams, or without a stream link
// are skipped (matches the user's "only things I can watch" requirement).
// Parse errors on a single block are logged but do not abort the whole run.
func Parse(htmlBody, game string, now time.Time, defaultBestOf int, defaultDuration time.Duration) []model.Match {
	blocks := strings.Split(htmlBody, `<div class="match-info">`)
	if len(blocks) < 2 {
		return nil
	}
	out := make([]model.Match, 0, len(blocks)-1)
	for i := 1; i < len(blocks); i++ {
		m, ok := parseBlock(blocks[i], game, now, defaultBestOf, defaultDuration)
		if ok {
			out = append(out, m)
		}
	}
	return out
}

// parseBlock extracts a single match from one HTML fragment. Returns
// (zero, false) if the block is missing any required field or the block
// doesn't contain a stream link.
func parseBlock(block, game string, now time.Time, defaultBestOf int, defaultDuration time.Duration) (model.Match, bool) {
	// Defend against parser panics from malformed input — worst case we
	// skip one block and keep going.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("liquipedia parser: recovered from panic in block: %v", r)
		}
	}()

	tsMatch := reTimestamp.FindStringSubmatch(block)
	if tsMatch == nil {
		return model.Match{}, false
	}
	tsUnix, err := strconv.ParseInt(tsMatch[1], 10, 64)
	if err != nil {
		return model.Match{}, false
	}
	startTime := time.Unix(tsUnix, 0).UTC()

	teamMatches := reTeam.FindAllStringSubmatch(block, -1)
	if len(teamMatches) < 2 {
		return model.Match{}, false
	}
	team1 := decodeEntities(teamMatches[0][1])
	team2 := decodeEntities(teamMatches[1][1])

	streamSubmatches := reStream.FindAllStringSubmatch(block, -1)
	if len(streamSubmatches) == 0 {
		return model.Match{}, false
	}
	streams := make([]model.Stream, 0, len(streamSubmatches))
	seen := make(map[string]struct{}, len(streamSubmatches))
	for _, sm := range streamSubmatches {
		platform := sm[1]
		// Liquipedia emits the channel twice: once in href (underscores)
		// and once in title (spaces). Normalize to the URL-safe form.
		channel := strings.ReplaceAll(sm[2], " ", "_")
		dedupeKey := platform + "|" + strings.ToLower(channel)
		if _, dup := seen[dedupeKey]; dup {
			continue
		}
		seen[dedupeKey] = struct{}{}
		streams = append(streams, model.Stream{
			Platform: platform,
			Channel:  channel,
			URL:      streamURLFor(platform, channel),
		})
	}

	tournamentName := ""
	if tm := reTournament.FindStringSubmatch(block); tm != nil {
		tournamentName = decodeEntities(strings.TrimSpace(tm[1]))
	}
	if tournamentName == "" {
		tournamentName = "Unknown tournament"
	}

	tier := ""
	if tm := reTier.FindStringSubmatch(block); tm != nil {
		tier = tierFromFilter(tm[1])
	}
	region := ""
	if rm := reRegion.FindStringSubmatch(block); rm != nil {
		region = rm[1]
	}

	status, s1, s2 := parseScore(block, startTime, now, defaultBestOf, defaultDuration)

	var teamA model.Team
	teamA.Name = team1
	if s1 != nil {
		teamA.Score = s1
	}
	var teamB model.Team
	teamB.Name = team2
	if s2 != nil {
		teamB.Score = s2
	}

	key := model.MatchKey("liquipedia", game, tsUnix, team1, team2, tournamentName)

	// Hash just the fields that meaningfully affect display so merges can
	// short-circuit when the upstream text hasn't changed.
	h := sha256.New()
	h.Write([]byte(key))
	h.Write([]byte(status.String()))
	if s1 != nil {
		fmt.Fprintf(h, "|%d", *s1)
	}
	if s2 != nil {
		fmt.Fprintf(h, "|%d", *s2)
	}
	for _, s := range streams {
		h.Write([]byte(s.URL))
	}
	blockHash := hex.EncodeToString(h.Sum(nil)[:8])

	return model.Match{
		Key:       key,
		Game:      game,
		StartTime: startTime,
		Teams:     [2]model.Team{teamA, teamB},
		Tournament: model.Tournament{
			Name:   tournamentName,
			Tier:   tier,
			Region: region,
		},
		BestOf:       defaultBestOf,
		Status:       status,
		StatusText:   status.String(),
		Streams:      streams,
		RawBlockHash: blockHash,
	}, true
}

// parseScore is the fix for the known rl-esports-tracker limitation:
// there, anything non-"vs" was treated as live/final indiscriminately.
// Here we:
//
//  1. Treat "vs" (and empty) as upcoming.
//  2. Try to recover both team scores from a nearby two-integer pattern
//     in the scoreholder siblings.
//  3. If we have two scores and now is past the expected match end
//     (start + bestOf*gameDuration), call it final. Otherwise live.
//  4. If we only have a single digit in the upper scoreholder, treat as
//     live but with partial scores.
func parseScore(block string, start, now time.Time, bestOf int, matchDuration time.Duration) (model.Status, *int, *int) {
	sm := reScoreSpan.FindStringSubmatch(block)
	text := ""
	if sm != nil {
		text = strings.TrimSpace(sm[1])
	}

	if text == "" || text == "vs" {
		if now.After(start) {
			// Scheduled time has passed but scoreholder still says "vs":
			// upstream hasn't caught up yet. Still treat as upcoming so we
			// don't lie to users. The poller will promote it once the
			// score appears.
			return model.StatusUpcoming, nil, nil
		}
		return model.StatusUpcoming, nil, nil
	}

	// Search the entire block for a two-integer pattern — Liquipedia
	// sometimes renders scores in sibling divs rather than the upper span.
	if pair := reScorePair.FindStringSubmatch(block); pair != nil {
		s1, _ := strconv.Atoi(pair[1])
		s2, _ := strconv.Atoi(pair[2])
		if s1 >= 0 && s2 >= 0 && (s1 > 0 || s2 > 0) {
			status := model.StatusLive
			// matchDuration is the expected total series duration, not per-game.
			// Give a generous grace window (2x) before declaring final purely
			// on wall-clock grounds.
			expectedEnd := start.Add(2 * matchDuration)
			if now.After(expectedEnd) {
				status = model.StatusFinal
			}
			// Best-of gating: in a BO5, first to 3 wins. If either team
			// has hit the win threshold, it's final regardless of clock.
			if bestOf > 0 {
				need := (bestOf / 2) + 1
				if s1 >= need || s2 >= need {
					status = model.StatusFinal
				}
			}
			return status, &s1, &s2
		}
	}

	// Non-"vs" text but no recoverable numeric scores — treat as live.
	// This matches the rl-tracker behavior for the edge case where the
	// upper scoreholder holds a symbol like a hyphen.
	return model.StatusLive, nil, nil
}

func streamURLFor(platform, channel string) string {
	switch platform {
	case "twitch":
		return "https://twitch.tv/" + channel
	case "youtube":
		// Liquipedia uses the bare channel name — modern YouTube prefers
		// the @handle format. Users can still click through and land on
		// the right channel.
		return "https://youtube.com/@" + channel
	default:
		return ""
	}
}

// tierFromFilter maps Liquipedia's tier filter numeric IDs to display
// names. This map is derived from the rocketleague wiki; other wikis
// use the same scheme.
func tierFromFilter(id string) string {
	switch id {
	case "1":
		return "S-Tier"
	case "2":
		return "A-Tier"
	case "3":
		return "B-Tier"
	case "4":
		return "C-Tier"
	case "5":
		return "D-Tier"
	case "-1":
		return "Misc"
	default:
		return ""
	}
}
