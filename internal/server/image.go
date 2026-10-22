package server

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/yangflow/oriel/internal/tracker"
)

// image relays a picture found inside a feed, so the interface never connects to such addresses itself.
func (s *Server) image(w http.ResponseWriter, r *http.Request) {
	address := r.URL.Query().Get("url")
	if !feedURL(address) {
		fail(w, http.StatusBadRequest, "invalid_url")
		return
	}
	if tracker.KnownAddress(address) {
		// A known tracker is refused as a blocked address is, and is not contacted.
		fail(w, http.StatusBadGateway, "image_unavailable")
		return
	}
	// The copy kept for a starred entry answers first, so a picture its site has taken down still shows.
	kind, body, found, err := s.store.ImageCopy(r.Context(), address)
	if err != nil {
		slog.Debug("Cannot read a picture copy", "error", err)
	}
	if !found {
		if s.images == nil {
			fail(w, http.StatusBadGateway, "image_unavailable")
			return
		}
		kind, body, err = s.images.Get(r.Context(), address)
		if err != nil {
			slog.Debug("Image unavailable", "error", err)
			fail(w, http.StatusBadGateway, "image_unavailable")
			return
		}
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.WriteHeader(http.StatusOK)
	w.Write(body)
}
