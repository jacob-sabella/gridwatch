package liquipedia

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jacob-sabella/gridwatch/internal/httpx"
	"github.com/jacob-sabella/gridwatch/internal/model"
	"github.com/jacob-sabella/gridwatch/internal/ratelimit"
)

// gameDefaults returns a minimal game slice suitable for tests.
func gameDefaults() []model.Game {
	return []model.Game{
		{Slug: "rocketleague", Display: "Rocket League", Color: "#1f6bff", DefaultBestOf: 5, MatchDuration: 90 * time.Minute},
	}
}

// stubServer returns an httptest.Server that serves the rocketleague
// snapshot on every /rocketleague/api.php request, optionally counting
// hits. Forces gzip and UA verification per request.
func stubServer(t *testing.T, hits *int64, seenUA *[]string, seenAccept *[]string) *httptest.Server {
	t.Helper()
	snapshot, err := os.ReadFile(filepath.Join("testdata", "rocketleague_matches.json"))
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(hits, 1)
		ua := r.Header.Get("User-Agent")
		ae := r.Header.Get("Accept-Encoding")
		if seenUA != nil {
			*seenUA = append(*seenUA, ua)
		}
		if seenAccept != nil {
			*seenAccept = append(*seenAccept, ae)
		}
		if !strings.HasPrefix(ua, "gridwatch/") || !strings.Contains(ua, "+") {
			t.Errorf("bad UA: %q", ua)
		}
		if ae != "gzip" {
			t.Errorf("bad Accept-Encoding: %q", ae)
		}
		// Serve plain (httpx client still accepts non-gzip bodies).
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(snapshot)
	}))
}

func TestFetchParsesRealSnapshot(t *testing.T) {
	var hits int64
	srv := stubServer(t, &hits, nil, nil)
	defer srv.Close()

	lim := ratelimit.New(100)
	lim.SetHostFloor(hostOf(t, srv.URL), 1*time.Millisecond)
	client := httpx.New("gridwatch/test (+test@example.com)", 5*time.Second)
	src := New(client, lim, gameDefaults()).WithEndpoint(srv.URL)
	// Override the limiter to track the test server's host too.
	src.limiter = lim

	// The source's Fetch uses Host constant, but the test hits a mock.
	// Override the rate-limit key just for this test.
	matches, err := src.Fetch(context.Background(), "rocketleague")
	if err != nil {
		// The rate limiter uses "liquipedia.net" as host key, not the mock's
		// host. So the limiter is essentially a no-op here — good, we're
		// testing Fetch + parser integration, not the limiter.
		t.Fatalf("Fetch: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("no matches parsed")
	}
	for _, m := range matches {
		if m.Source != "liquipedia" {
			t.Errorf("Source field not set: %q", m.Source)
		}
	}
	if hits != 1 {
		t.Errorf("expected 1 upstream hit, got %d", hits)
	}
}

func TestFetchHonorsRateLimitFloor(t *testing.T) {
	var hits int64
	srv := stubServer(t, &hits, nil, nil)
	defer srv.Close()

	// Build a limiter with a 200ms floor keyed to the liquipedia Host.
	lim := ratelimit.New(100)
	lim.SetHostFloor(rateKey("rocketleague"), 200*time.Millisecond)
	client := httpx.New("gridwatch/test (+x@y)", 5*time.Second)
	src := New(client, lim, gameDefaults()).WithEndpoint(srv.URL)

	ctx := context.Background()
	start := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = src.Fetch(ctx, "rocketleague")
		}()
	}
	wg.Wait()

	elapsed := time.Since(start)
	// 3 fetches at 200ms floor → first free, then ~200ms, ~200ms = ~400ms min
	if elapsed < 350*time.Millisecond {
		t.Errorf("rate-limit floor not honored: elapsed %s", elapsed)
	}
	if hits != 3 {
		t.Errorf("expected 3 hits, got %d", hits)
	}
}

func TestFetchHandles429WithBackoff(t *testing.T) {
	var hits int64
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"parse":{"title":"x","pageid":1,"text":{"*":"<html/>"}}}`))
	}))
	defer srv.Close()

	lim := ratelimit.New(100)
	lim.SetHostFloor(rateKey("rocketleague"), 1*time.Millisecond)
	client := httpx.New("gridwatch/test (+x@y)", 5*time.Second)
	src := New(client, lim, gameDefaults()).WithEndpoint(srv.URL)

	// First call should return ErrRateLimited.
	_, err := src.Fetch(context.Background(), "rocketleague")
	if err == nil {
		t.Fatal("expected error on first 429")
	}
	var rl *httpx.ErrRateLimited
	if !asErr(err, &rl) {
		t.Fatalf("expected ErrRateLimited, got %T: %v", err, err)
	}
	if rl.RetryAfter != 2*time.Second {
		t.Errorf("RetryAfter: got %s", rl.RetryAfter)
	}

	// Second call should return ErrCooldown from the limiter.
	_, err = src.Fetch(context.Background(), "rocketleague")
	if err == nil {
		t.Fatal("expected cooldown error on second call")
	}
	var cd *ratelimit.ErrCooldown
	if !asErr(err, &cd) {
		t.Fatalf("expected ErrCooldown, got %T: %v", err, err)
	}
}

func TestFetchHandles500WithBackoff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	lim := ratelimit.New(100)
	lim.SetHostFloor(rateKey("rocketleague"), 1*time.Millisecond)
	client := httpx.New("gridwatch/test (+x@y)", 5*time.Second)
	src := New(client, lim, gameDefaults()).WithEndpoint(srv.URL)

	_, err := src.Fetch(context.Background(), "rocketleague")
	if err == nil {
		t.Fatal("expected error on 500")
	}
	if lim.Cooldown(rateKey("rocketleague")) < 9*time.Minute {
		t.Errorf("500 should trigger ≥9m cooldown, got %s", lim.Cooldown(rateKey("rocketleague")))
	}
}

func TestFetchRejectsUnknownGame(t *testing.T) {
	lim := ratelimit.New(100)
	client := httpx.New("gridwatch/test (+x@y)", 5*time.Second)
	src := New(client, lim, gameDefaults())
	_, err := src.Fetch(context.Background(), "notagame")
	if err == nil || !strings.Contains(err.Error(), "unknown game") {
		t.Errorf("expected unknown game error, got %v", err)
	}
}

// asErr is a tiny errors.As wrapper that only works on pointer-to-pointer
// targets but keeps the tests self-contained.
func asErr(err error, target any) bool {
	if err == nil {
		return false
	}
	type unwrapper interface{ Unwrap() error }
	switch t := target.(type) {
	case **httpx.ErrRateLimited:
		var v *httpx.ErrRateLimited
		for e := err; e != nil; {
			if cast, ok := e.(*httpx.ErrRateLimited); ok {
				v = cast
				break
			}
			if u, ok := e.(unwrapper); ok {
				e = u.Unwrap()
				continue
			}
			break
		}
		if v != nil {
			*t = v
			return true
		}
	case **ratelimit.ErrCooldown:
		var v *ratelimit.ErrCooldown
		for e := err; e != nil; {
			if cast, ok := e.(*ratelimit.ErrCooldown); ok {
				v = cast
				break
			}
			if u, ok := e.(unwrapper); ok {
				e = u.Unwrap()
				continue
			}
			break
		}
		if v != nil {
			*t = v
			return true
		}
	}
	return false
}

func hostOf(t *testing.T, u string) string {
	t.Helper()
	parsed, err := url.Parse(u)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	return parsed.Host
}

var _ = fmt.Sprintf
