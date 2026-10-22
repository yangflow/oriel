package server

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/yangflow/oriel/internal/fetch"
)

// findFeeds answers GET /api/discover?url=: the feeds a web address leads to. It only looks; nothing is stored.
func (s *Server) findFeeds(w http.ResponseWriter, r *http.Request) {
	address := r.URL.Query().Get("url")
	if !feedURL(address) {
		fail(w, http.StatusBadRequest, "invalid_url")
		return
	}
	if s.discover == nil {
		fail(w, http.StatusBadGateway, "page_unavailable")
		return
	}
	found, err := s.discover.Discover(r.Context(), address)
	switch {
	case errors.Is(err, fetch.ErrInvalidAddress):
		fail(w, http.StatusBadRequest, "invalid_url")
	case errors.Is(err, fetch.ErrBlockedAddress):
		fail(w, http.StatusUnprocessableEntity, "address_not_public")
	case err != nil:
		slog.Debug("Page unavailable", "error", err)
		fail(w, http.StatusBadGateway, "page_unavailable")
	default:
		if found == nil {
			found = []fetch.Candidate{}
		}
		reply(w, http.StatusOK, map[string]any{"candidates": found})
	}
}
