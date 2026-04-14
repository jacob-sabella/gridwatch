package server

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

// SSEHub is a tiny fan-out for store revision bumps.
// Clients subscribe on connect, receive int64 revisions, and are dropped
// on disconnect.
type SSEHub struct {
	mu      sync.Mutex
	clients map[chan int64]struct{}
}

// NewSSEHub returns an empty hub.
func NewSSEHub() *SSEHub {
	return &SSEHub{clients: make(map[chan int64]struct{})}
}

// Subscribe adds a listener channel to the hub. Caller must call
// Unsubscribe on cleanup. Channel is buffered so one slow client can't
// block broadcasts.
func (h *SSEHub) Subscribe() chan int64 {
	ch := make(chan int64, 4)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

// Unsubscribe removes a listener.
func (h *SSEHub) Unsubscribe(ch chan int64) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
	close(ch)
}

// Broadcast pushes rev to every subscriber. Non-blocking: if a client's
// buffer is full, we drop the update for that client.
func (h *SSEHub) Broadcast(rev int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- rev:
		default:
		}
	}
}

// handleSSE is the /events endpoint. It writes text/event-stream with
// a heartbeat every 25s and pushes revision bumps as they arrive.
//
// Reverse-proxy gotcha: nginx buffers SSE by default. We set
// X-Accel-Buffering: no to override; NPM's openresty honors this.
// Caddy and Traefik pass SSE through without buffering natively.
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// Initial heartbeat + current revision so the client can refresh
	// on first connection if it reconnected after a drop.
	fmt.Fprintf(w, ": connected\n\n")
	fmt.Fprintf(w, "event: revision\ndata: %d\n\n", s.store.Revision())
	flusher.Flush()

	ch := s.hub.Subscribe()
	defer s.hub.Unsubscribe(ch)

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case rev, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: revision\ndata: %d\n\n", rev)
			flusher.Flush()
		case <-heartbeat.C:
			// Heartbeat comment; browsers use this as a liveness signal
			// and proxies use it to avoid closing idle connections.
			fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}
