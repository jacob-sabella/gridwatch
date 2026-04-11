package liquipedia

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/jsabella/gridwatch/internal/httpx"
	"github.com/jsabella/gridwatch/internal/model"
	"github.com/jsabella/gridwatch/internal/ratelimit"
	"github.com/jsabella/gridwatch/internal/source"
)

// Host is the hostname prefix the rate limiter uses for per-page floors.
// The real rate-limit key is "liquipedia.net/<game>" so each wiki page
// has its own ≥90s floor, which matches Liquipedia's per-page ToU.
const Host = "liquipedia.net"

// rateKey returns the per-(host, game) rate-limit bucket key.
func rateKey(game string) string { return Host + "/" + game }

// DefaultInterval is the polite floor between fetches for the same wiki
// page. Liquipedia's ToU requires conservative polling for the parse API.
const DefaultInterval = 90 * time.Second

// Source implements source.Source for Liquipedia wikis.
type Source struct {
	client    *httpx.Client
	limiter   *ratelimit.Limiter
	games     []model.Game
	endpoint  string // overridable for tests
	interval  time.Duration
}

// New wires a Liquipedia source with the given HTTP client, rate limiter,
// and per-game metadata. The rate limiter must already have the
// Liquipedia host floor configured (Host → DefaultInterval).
func New(client *httpx.Client, limiter *ratelimit.Limiter, games []model.Game) *Source {
	return &Source{
		client:   client,
		limiter:  limiter,
		games:    games,
		endpoint: "https://liquipedia.net",
		interval: DefaultInterval,
	}
}

// WithEndpoint overrides the base URL. Used in tests to aim at httptest.
func (s *Source) WithEndpoint(u string) *Source {
	s.endpoint = u
	return s
}

// WithInterval overrides the poll floor. Used in tests.
func (s *Source) WithInterval(d time.Duration) *Source {
	s.interval = d
	return s
}

// Name implements source.Source.
func (s *Source) Name() string { return "liquipedia" }

// Games implements source.Source.
func (s *Source) Games() []string {
	out := make([]string, 0, len(s.games))
	for _, g := range s.games {
		out = append(out, g.Slug)
	}
	return out
}

// MinInterval implements source.Source.
func (s *Source) MinInterval() time.Duration { return s.interval }

// Fetch implements source.Source. Applies the rate limiter, performs a
// single parse-API request, and delegates to Parse.
func (s *Source) Fetch(ctx context.Context, game string) ([]model.Match, error) {
	meta, ok := s.gameMeta(game)
	if !ok {
		return nil, fmt.Errorf("liquipedia: unknown game %q", game)
	}

	// Rate limit gate — keyed per (host, game) so different wiki pages
	// can be polled in parallel, but each individual page still honors
	// the per-page floor from Liquipedia's ToU.
	if err := s.limiter.Wait(ctx, rateKey(game)); err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/%s/api.php?action=parse&page=Liquipedia:Matches&format=json&prop=text",
		s.endpoint, game)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
		s.limiter.ReportBackoff(rateKey(game), retryAfter)
		return nil, &httpx.ErrRateLimited{RetryAfter: retryAfter, Upstream: Host}
	}
	if resp.StatusCode >= 500 {
		// Discard body so the connection can be reused.
		_, _ = io.Copy(io.Discard, resp.Body)
		s.limiter.ReportBackoff(rateKey(game), 10*time.Minute)
		return nil, &httpx.ErrUpstream{Status: resp.StatusCode, URL: url}
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024))
		return nil, &httpx.ErrUpstream{Status: resp.StatusCode, URL: url, Body: string(body)}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	html, err := DecodeResponse(body)
	if err != nil {
		return nil, err
	}

	matches := Parse(html, game, time.Now().UTC(), meta.DefaultBestOf, meta.MatchDuration)
	for i := range matches {
		matches[i].Source = "liquipedia"
	}
	return matches, nil
}

func (s *Source) gameMeta(game string) (model.Game, bool) {
	for _, g := range s.games {
		if g.Slug == game {
			return g, true
		}
	}
	return model.Game{}, false
}

// parseRetryAfter reads the Retry-After header. The server can give
// either a delta-seconds integer or an HTTP date; we support the former
// and fall back to 10 minutes otherwise (matches the spec).
func parseRetryAfter(h string) time.Duration {
	if h == "" {
		return 10 * time.Minute
	}
	if secs, err := strconv.Atoi(h); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(h); err == nil {
		d := time.Until(t)
		if d > 0 {
			return d
		}
	}
	return 10 * time.Minute
}

// Compile-time check that Source satisfies source.Source.
var _ source.Source = (*Source)(nil)
