package server

import (
	"net/http"
)

// render writes the named template with the given data to w.
// On error, logs and sends a 500. Used by both partial and full-page
// handlers; HTMX detection happens at the caller level.
func (s *Server) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := s.templ.Execute(w, name, data); err != nil {
		s.log.Error("template render failed", "template", name, "err", err)
		// If we've already written headers, body is already flushed with
		// whatever partial content — nothing more we can do.
	}
}
