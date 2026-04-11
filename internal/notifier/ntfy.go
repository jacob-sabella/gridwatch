package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jsabella/gridwatch/internal/config"
)

// NtfySink implements Sink against ntfy servers using the JSON publish
// format. We use JSON (POST /) rather than header-based publishing
// because HTTP headers in Go's stdlib (and Node.js) reject UTF-8
// characters — a bug we hit during the rl-esports-tracker session where
// emoji titles caused ERR_INVALID_CHAR.
type NtfySink struct {
	name     string
	url      string
	topic    string
	user     string
	password string
	priority map[string]int
	client   *http.Client
}

// NewNtfySink builds a sink from a config entry. Validation has already
// happened in config.Validate so url/topic are guaranteed non-empty.
func NewNtfySink(c config.NotificationSink) *NtfySink {
	return &NtfySink{
		name:     "ntfy(" + c.URL + ")",
		url:      c.URL,
		topic:    c.Topic,
		user:     c.User,
		password: c.Password,
		priority: c.PriorityMap,
		client:   &http.Client{Timeout: 10 * time.Second},
	}
}

// Name implements Sink.
func (s *NtfySink) Name() string { return s.name }

// Deliver implements Sink. Returns nil only on HTTP 2xx.
func (s *NtfySink) Deliver(ctx context.Context, ev Event) error {
	payload := map[string]any{
		"topic":    s.topic,
		"title":    ev.Title,
		"message":  ev.Body,
		"priority": resolvePriority(ev, s.priority),
		"tags":     ev.Tags,
	}
	if ev.Click != "" {
		payload["click"] = ev.Click
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.url+"/", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.user != "" {
		req.SetBasicAuth(s.user, s.password)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("ntfy %d: %s", resp.StatusCode, string(snippet))
	}
	return nil
}

// resolvePriority picks a priority for the event, honoring a user-configured
// stage → priority map, falling back to the event's default.
func resolvePriority(ev Event, m map[string]int) int {
	if m != nil {
		if p, ok := m[string(ev.Stage)]; ok && p > 0 {
			return p
		}
		if p, ok := m["default"]; ok && p > 0 {
			return p
		}
	}
	if ev.Priority > 0 {
		return ev.Priority
	}
	return 3
}
