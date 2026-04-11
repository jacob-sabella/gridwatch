// Package server is the HTTP layer: routes, handlers, templates, SSE,
// and reverse-proxy base-URL support. Uses only stdlib + the embedded
// ui package.
package server

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/jsabella/gridwatch/internal/config"
	"github.com/jsabella/gridwatch/internal/model"
	"github.com/jsabella/gridwatch/internal/store"
	"github.com/jsabella/gridwatch/internal/ui"
)

// Server wires the HTTP layer to the store. It's stateless beyond the
// store reference — the mux is built in Handler() and can be re-built
// for tests.
type Server struct {
	cfg     *config.Config
	store   *store.Store
	games   []model.Game
	log     *slog.Logger
	hub     *SSEHub
	templ   *ui.Templates
	baseURL string
	startAt string
}

// Config is what main.go passes in.
type Config struct {
	AppConfig *config.Config
	Store     *store.Store
	Games     []model.Game
	Logger    *slog.Logger
	Version   string
}

// New builds a Server ready for Handler().
func New(cfg Config) (*Server, error) {
	templ, err := ui.Load()
	if err != nil {
		return nil, err
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	s := &Server{
		cfg:     cfg.AppConfig,
		store:   cfg.Store,
		games:   cfg.Games,
		log:     log,
		templ:   templ,
		hub:     NewSSEHub(),
		baseURL: strings.TrimRight(cfg.AppConfig.BaseURL, "/"),
		startAt: cfg.Version,
	}
	// Wire the hub to the store's transition channel so that any store
	// update advances the revision broadcast.
	go s.runHubWatcher()
	return s, nil
}

// Handler returns the *http.ServeMux configured with all routes and
// middleware applied. Designed so integration tests can hit handlers
// without spinning a real listener.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.registerRoutes(mux)

	var h http.Handler = mux
	h = s.recover(h)
	h = s.requestLog(h)
	if s.baseURL != "" {
		h = http.StripPrefix(s.baseURL, h)
	}
	return h
}

// runHubWatcher listens for store transitions and broadcasts revision
// bumps to SSE clients. A no-op broadcast is cheap.
func (s *Server) runHubWatcher() {
	for range s.store.Transitions() {
		s.hub.Broadcast(s.store.Revision())
	}
}

// recover is a panic-catching middleware that returns 500 without
// crashing the process.
func (s *Server) recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("panic in handler", "err", rec, "path", r.URL.Path)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// requestLog logs every request at debug level. Lightweight; no per-request
// allocations beyond what slog does.
func (s *Server) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.log.Debug("request",
			"method", r.Method, "path", r.URL.Path, "remote", r.RemoteAddr)
		next.ServeHTTP(w, r)
	})
}

// isHTMX returns true if the request was made by htmx (as opposed to a
// plain browser navigation). Handlers render partials instead of full
// pages when this is true.
func isHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}
