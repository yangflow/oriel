// Package server is the HTTP API. Clients, including the native window, read and write data only through it.
package server

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yangflow/oriel/internal/ai"
	"github.com/yangflow/oriel/internal/fetch"
	"github.com/yangflow/oriel/internal/store"
)

const (
	cookieName = "oriel_session"
	// sessionIdle is how long an unused session stays valid; it is also the cookie lifetime.
	sessionIdle = 30 * 24 * time.Hour
	// touchEvery limits last-seen writes to one per session per minute.
	touchEvery = time.Minute
	keyLength  = 32
)

// passwordRounds is a variable so tests can make logins cheap.
var passwordRounds = 600_000

// Config holds the startup options of the server.
type Config struct {
	// Password enables authentication and listening on every interface. Empty means local-only, no sign-in.
	Password string
	Version  string
	// Images downloads pictures for GET /api/image; nil answers every request with image_unavailable.
	Images Images
	// Discover finds the feeds of a web address for GET /api/discover; nil answers every request with page_unavailable.
	Discover Discoverer
	// Routes finds and previews RSSHub routes for GET /api/routes; nil makes the finder of fetch.
	Routes RouteFinder
	// AI works through the queue of summaries and translations. Nil makes one that is never run, so
	// requests stay queued.
	AI *ai.Worker
	// Fetching returns the ids of the sources being fetched now, for GET /api/activity; nil means none are.
	Fetching func() []int64
}

// Images downloads a picture found inside a feed and returns its content type and bytes.
type Images interface {
	Get(ctx context.Context, address string) (string, []byte, error)
}

// Discoverer finds the feeds a web address leads to. It returns fetch.ErrInvalidAddress, fetch.ErrBlockedAddress or
// fetch.ErrPageUnavailable for the failures the API tells apart.
type Discoverer interface {
	Discover(ctx context.Context, address string) ([]fetch.Candidate, error)
}

// RouteFinder looks up the routes of the RSSHub instances and previews one. Find never fails: a catalog that cannot
// be read is Available false. Preview returns fetch.ErrNoInstance, ErrRouteNotFound, ErrRouteUnavailable or ErrNotAFeed.
type RouteFinder interface {
	Find(ctx context.Context, instances []string, q string, refresh bool) fetch.RouteMatches
	Preview(ctx context.Context, instances []string, path string) (fetch.RoutePreview, error)
}

// Server serves the HTTP API on top of a store.
type Server struct {
	store    *store.Store
	images   Images
	discover Discoverer
	routes   RouteFinder
	ai       *ai.Worker
	version  string
	salt     []byte
	derived  []byte // nil when no password is set
	now      func() time.Time
	throttle *throttle
	handler  http.Handler
	fetching func() []int64

	vacuumMu sync.Mutex // guards vacuum
	vacuum   vacuumState
}

// New derives the password key once and builds the routes.
func New(st *store.Store, cfg Config) (*Server, error) {
	s := &Server{store: st, images: cfg.Images, discover: cfg.Discover, routes: cfg.Routes, ai: cfg.AI, fetching: cfg.Fetching, version: cfg.Version, now: time.Now, throttle: newThrottle(), vacuum: vacuumState{State: "idle"}}
	if s.routes == nil {
		s.routes = fetch.NewRoutes(cfg.Version)
	}
	if s.ai == nil {
		s.ai = ai.New(st, cfg.Version)
	}
	if cfg.Password != "" {
		s.salt = make([]byte, 16)
		rand.Read(s.salt)
		derived, err := derive(cfg.Password, s.salt)
		if err != nil {
			return nil, err
		}
		s.derived = derived
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("POST /auth/login", s.login)
	mux.HandleFunc("POST /auth/logout", s.authed(s.logout))
	mux.HandleFunc("GET /api/sessions", s.authed(s.sessions))
	mux.HandleFunc("DELETE /api/sessions/{id}", s.authed(s.revoke))
	mux.HandleFunc("GET /websub/{key}", s.websub)
	mux.HandleFunc("POST /websub/{key}", s.websub)
	mux.HandleFunc("GET /api/sources", s.authed(s.listSources))
	mux.HandleFunc("POST /api/sources", s.authed(s.createSource))
	mux.HandleFunc("GET /api/sources/health", s.authed(s.sourcesHealth))
	mux.HandleFunc("GET /api/sources/{id}", s.authed(s.getSource))
	mux.HandleFunc("PATCH /api/sources/{id}", s.authed(s.updateSource))
	mux.HandleFunc("DELETE /api/sources/{id}", s.authed(s.deleteSource))
	mux.HandleFunc("POST /api/sources/{id}/muted-count", s.authed(s.mutedCount))
	mux.HandleFunc("POST /api/sources/refresh", s.authed(s.refreshAll))
	mux.HandleFunc("POST /api/sources/{id}/refresh", s.authed(s.refreshSource))
	mux.HandleFunc("GET /api/entries", s.authed(s.listEntries))
	mux.HandleFunc("GET /api/entries/{id}", s.authed(s.getEntry))
	mux.HandleFunc("PATCH /api/entries/{id}", s.authed(s.updateEntry))
	mux.HandleFunc("POST /api/entries/read", s.authed(s.markRead))
	mux.HandleFunc("POST /api/entries/unread", s.authed(s.markUnread))
	mux.HandleFunc("GET /api/entries/{id}/ai", s.authed(s.getAI))
	mux.HandleFunc("POST /api/entries/{id}/extract", s.authed(s.retryExtraction))
	mux.HandleFunc("POST /api/entries/{id}/summary", s.authed(s.requestSummary))
	mux.HandleFunc("POST /api/entries/{id}/translation", s.authed(s.requestTranslation))
	mux.HandleFunc("POST /api/ai/test", s.authed(s.testAI))
	mux.HandleFunc("POST /api/ai/selection", s.authed(s.selectAI))
	mux.HandleFunc("GET /api/settings", s.authed(s.getSettings))
	mux.HandleFunc("PATCH /api/settings", s.authed(s.updateSettings))
	mux.HandleFunc("POST /api/opml/import", s.authed(s.importOPML))
	mux.HandleFunc("GET /api/opml/export", s.authed(s.exportOPML))
	mux.HandleFunc("GET /api/image", s.authed(s.image))
	mux.HandleFunc("GET /api/activity", s.authed(s.activity))
	mux.HandleFunc("GET /api/storage", s.authed(s.storage))
	mux.HandleFunc("POST /api/storage/vacuum", s.authed(s.startVacuum))
	mux.HandleFunc("GET /api/discover", s.authed(s.findFeeds))
	mux.HandleFunc("GET /api/routes", s.authed(s.findRoutes))
	mux.HandleFunc("GET /api/routes/preview", s.authed(s.previewRoute))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { fail(w, http.StatusNotFound, "not_found") })
	s.handler = mux
	return s, nil
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.handler.ServeHTTP(w, r) }

// ListenAddr keeps a server without a password on the loopback interface.
func ListenAddr(password string, port int) string {
	host := "127.0.0.1"
	if password != "" {
		host = ""
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

func derive(password string, salt []byte) ([]byte, error) {
	return pbkdf2.Key(sha256.New, password, salt, passwordRounds, keyLength)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	status, database := http.StatusOK, "ok"
	if err := s.store.Writable(r.Context()); err != nil {
		slog.Error("Database is not writable", "error", err)
		status, database = http.StatusServiceUnavailable, "unwritable"
	}
	reply(w, status, map[string]any{
		"app": "oriel", "version": s.version, "auth": s.derived != nil, "database": database,
	})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if s.derived == nil {
		fail(w, http.StatusBadRequest, "auth_disabled")
		return
	}
	key, now := clientKey(r), s.now()
	if wait := s.throttle.wait(key, now); wait > 0 {
		tooMany(w, wait)
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	// A malformed body is treated as a wrong password, never as a field validation error.
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		s.throttle.fail(key, now)
		fail(w, http.StatusUnauthorized, "wrong_password")
		return
	}
	// Deriving the key is slow on purpose, so only a few run at once; the rest are turned away, not queued.
	if !s.throttle.acquire() {
		tooMany(w, time.Second)
		return
	}
	derived, err := derive(body.Password, s.salt)
	s.throttle.release()
	if err != nil || subtle.ConstantTimeCompare(derived, s.derived) != 1 {
		s.throttle.fail(key, now)
		fail(w, http.StatusUnauthorized, "wrong_password")
		return
	}
	s.throttle.succeed(key)
	if err := s.store.DeleteSessionsIdleSince(r.Context(), now.Add(-sessionIdle)); err != nil {
		internal(w, err)
		return
	}
	raw := make([]byte, 32)
	rand.Read(raw)
	token := base64.RawURLEncoding.EncodeToString(raw)
	if _, err := s.store.CreateSession(r.Context(), hash(token), r.UserAgent(), now); err != nil {
		internal(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: token, Path: "/", MaxAge: int(sessionIdle / time.Second),
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: secure(r),
	})
	reply(w, http.StatusOK, map[string]string{"token": token})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if id, ok := sessionID(r.Context()); ok {
		if err := s.store.DeleteSession(r.Context(), id); err != nil && !errors.Is(err, store.ErrNotFound) {
			internal(w, err)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: secure(r),
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) sessions(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.Sessions(r.Context(), s.now().Add(-sessionIdle))
	if err != nil {
		internal(w, err)
		return
	}
	current, _ := sessionID(r.Context())
	type item struct {
		ID         int64  `json:"id"`
		UserAgent  string `json:"userAgent"`
		CreatedAt  int64  `json:"createdAt"`
		LastSeenAt int64  `json:"lastSeenAt"`
		Current    bool   `json:"current"`
	}
	items := make([]item, len(list))
	for i, session := range list {
		items[i] = item{session.ID, session.UserAgent, session.CreatedAt.UnixMilli(), session.LastSeenAt.UnixMilli(), session.ID == current}
	}
	reply(w, http.StatusOK, map[string]any{"sessions": items})
}

func (s *Server) revoke(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	switch err := s.store.DeleteSession(r.Context(), id); {
	case errors.Is(err, store.ErrNotFound):
		fail(w, http.StatusNotFound, "not_found")
	case err != nil:
		internal(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

type sessionKey struct{}

func sessionID(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(sessionKey{}).(int64)
	return id, ok
}

// authed requires a live session when a password is set, and records its use at most once a minute.
func (s *Server) authed(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.derived == nil {
			next(w, r)
			return
		}
		token := ""
		if bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
			token = bearer
		} else if cookie, err := r.Cookie(cookieName); err == nil {
			token = cookie.Value
		}
		if token == "" {
			fail(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		session, err := s.store.SessionByToken(r.Context(), hash(token))
		now := s.now()
		if errors.Is(err, store.ErrNotFound) || (err == nil && now.Sub(session.LastSeenAt) >= sessionIdle) {
			fail(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if err != nil {
			internal(w, err)
			return
		}
		if now.Sub(session.LastSeenAt) >= touchEvery {
			if err := s.store.TouchSession(r.Context(), session.ID, now); err != nil {
				internal(w, err)
				return
			}
		}
		next(w, r.WithContext(context.WithValue(r.Context(), sessionKey{}, session.ID)))
	}
}

func hash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func secure(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func reply(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

func fail(w http.ResponseWriter, status int, code string) {
	reply(w, status, map[string]string{"error": code})
}

func internal(w http.ResponseWriter, err error) {
	slog.Error("Request failed", "error", err)
	fail(w, http.StatusInternalServerError, "internal")
}
