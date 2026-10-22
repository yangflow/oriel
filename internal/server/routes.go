package server

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/yangflow/oriel/internal/fetch"
)

// instances reads the RSSHub instances of the settings.
func (s *Server) instances(r *http.Request) ([]string, error) {
	raw, err := s.store.Setting(r.Context(), fetch.RSSHubSetting)
	if err != nil {
		return nil, err
	}
	return fetch.Instances(raw), nil
}

// findRoutes answers GET /api/routes?q=&refresh=1: the routes of the user's RSSHub instance that fit a link or a
// name. A catalog that cannot be read is not an error; it only says so. Nothing is stored.
func (s *Server) findRoutes(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		fail(w, http.StatusBadRequest, "invalid_query")
		return
	}
	instances, err := s.instances(r)
	if err != nil {
		internal(w, err)
		return
	}
	reply(w, http.StatusOK, s.routes.Find(r.Context(), instances, q, r.URL.Query().Get("refresh") == "1"))
}

// previewRoute answers GET /api/routes/preview?path=: the first entries of a route, without subscribing.
func (s *Server) previewRoute(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		fail(w, http.StatusBadRequest, "invalid_rsshub_path")
		return
	}
	instances, err := s.instances(r)
	if err != nil {
		internal(w, err)
		return
	}
	got, err := s.routes.Preview(r.Context(), instances, path)
	switch {
	case errors.Is(err, fetch.ErrNoInstance):
		fail(w, http.StatusUnprocessableEntity, "no_rsshub_instance")
	case errors.Is(err, fetch.ErrRouteNotFound):
		fail(w, http.StatusBadGateway, "route_not_found")
	case errors.Is(err, fetch.ErrNotAFeed):
		fail(w, http.StatusBadGateway, "not_a_feed")
	case err != nil:
		slog.Debug("Route unavailable", "error", err)
		fail(w, http.StatusBadGateway, "route_unavailable")
	default:
		reply(w, http.StatusOK, got)
	}
}
