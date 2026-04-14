package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jacob-sabella/gridwatch/internal/config"
)

// WebhookSink POSTs a JSON payload to a user-supplied URL. The payload
// includes the match, stage, and rendered title/body — consumers can
// route it anywhere (Discord, Slack, a custom endpoint).
type WebhookSink struct {
	name    string
	url     string
	method  string
	headers map[string]string
	client  *http.Client
}

// NewWebhookSink builds a sink from config.
func NewWebhookSink(c config.NotificationSink) *WebhookSink {
	method := strings.ToUpper(c.Method)
	if method == "" {
		method = "POST"
	}
	return &WebhookSink{
		name:    "webhook(" + c.URL + ")",
		url:     c.URL,
		method:  method,
		headers: c.Headers,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

// Name implements Sink.
func (s *WebhookSink) Name() string { return s.name }

// Deliver implements Sink.
func (s *WebhookSink) Deliver(ctx context.Context, ev Event) error {
	body, err := json.Marshal(map[string]any{
		"stage":      string(ev.Stage),
		"title":      ev.Title,
		"message":    ev.Body,
		"click":      ev.Click,
		"priority":   ev.Priority,
		"tags":       ev.Tags,
		"match":      ev.Match,
	})
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, s.method, s.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range s.headers {
		req.Header.Set(k, v)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("webhook %d: %s", resp.StatusCode, string(snippet))
	}
	return nil
}
