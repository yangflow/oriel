package server

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/yangflow/oriel/internal/fetch"
	"github.com/yangflow/oriel/internal/store"
)

// candidateJSON is a candidate with the subscription that already has the same feed, 0 when none does.
type candidateJSON struct {
	fetch.Candidate
	SubscribedID int64 `json:"subscribedId"`
}

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
		candidates := make([]candidateJSON, len(found))
		for i, c := range found {
			candidates[i] = candidateJSON{Candidate: c}
			id, _, err := s.store.FindDuplicate(r.Context(), store.NewSource{URL: c.URL, RSSHubPath: c.RSSHubPath})
			if err != nil {
				internal(w, err)
				return
			}
			candidates[i].SubscribedID = id
		}
		reply(w, http.StatusOK, map[string]any{"candidates": candidates})
	}
}
