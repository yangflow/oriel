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
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yangflow/oriel/internal/ai"
	"github.com/yangflow/oriel/internal/autobackup"
	"github.com/yangflow/oriel/internal/diag"
	"github.com/yangflow/oriel/internal/fetch"
	"github.com/yangflow/oriel/internal/importer"
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
	// InstanceHealth returns how the latest requests to each of the RSSHub instances went, for GET /api/rsshub/health;
	// nil means there is no data.
	InstanceHealth func(instances []string) []fetch.InstanceHealth
	// Log holds the recent warnings and errors for GET /api/diagnostics; nil says the file has none to show.
	Log *diag.Handler
	// DataDir is where the log file lives, whose end GET /api/diagnostics shows; empty says there is none.
	DataDir string
	// Mode is diag.ModeDesktop or diag.ModeHeadless.
	Mode string
	// Pause stops the fetch loop and the AI worker, and waits until they have, while a restore replaces the data. The
	// function it returns starts them again. Nil means there is nothing to stop.
	Pause func(ctx context.Context) (resume func(), err error)
	// AutoBackup is the scheduler of automatic backups. The server tells it to check after the settings changed and
	// keeps it from starting during a restore or a vacuum; whoever owns it runs it. Nil means there is none.
	AutoBackup *autobackup.Runner
	// TrustedProxies are the reverse proxies whose X-Forwarded-For the login throttle believes. Empty means
	// RemoteAddr is always the client.
	TrustedProxies []netip.Prefix
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
	store          *store.Store
	images         Images
	discover       Discoverer
	routes         RouteFinder
	ai             *ai.Worker
	version        string
	salt           []byte
	derived        []byte // nil when no password is set
	now            func() time.Time
	throttle       *throttle
	proxies        []netip.Prefix
	probe          writeProbe
	writable       func(context.Context) error // the write probe of /health; tests replace it
	handler        http.Handler
	fetching       func() []int64
	instanceHealth func(instances []string) []fetch.InstanceHealth
	log            *diag.Handler
	dataDir        string
	mode           string

	imports      importState
	opml         *opmlState
	importClient func() *importer.Client // makes the client of one import; tests replace it

	settingsMu sync.Mutex // serializes settings updates

	vacuumMu sync.Mutex // guards vacuum
	vacuum   vacuumState

	restoring sync.Mutex // held for the length of a restore
	pause     func(ctx context.Context) (resume func(), err error)

	auto *autobackup.Runner
}

// New derives the password key once and builds the routes.
func New(st *store.Store, cfg Config) (*Server, error) {
	s := &Server{store: st, images: cfg.Images, discover: cfg.Discover, routes: cfg.Routes, ai: cfg.AI, fetching: cfg.Fetching, instanceHealth: cfg.InstanceHealth, log: cfg.Log, dataDir: cfg.DataDir, mode: cfg.Mode, pause: cfg.Pause, auto: cfg.AutoBackup, version: cfg.Version, now: time.Now, importClient: importer.NewClient, opml: newOPMLState(), throttle: newThrottle(), proxies: cfg.TrustedProxies, vacuum: vacuumState{State: "idle"}}
	if s.routes == nil {
		s.routes = fetch.NewRoutes(cfg.Version)
	}
	if s.ai == nil {
		s.ai = ai.New(st, cfg.Version)
	}
	if s.auto != nil {
		s.auto.Busy = s.maintenance
	}
	s.writable = st.Writable
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
	mux.HandleFunc("POST /api/sources/{id}/noisy/dismiss", s.authed(s.dismissNoisy))
	mux.HandleFunc("DELETE /api/sources/{id}", s.authed(s.deleteSource))
	mux.HandleFunc("POST /api/sources/{id}/restore", s.authed(s.restoreSource))
	mux.HandleFunc("POST /api/sources/{id}/move", s.authed(s.moveSource))
	mux.HandleFunc("POST /api/sources/{id}/dismiss-move", s.authed(s.dismissMove))
	mux.HandleFunc("POST /api/sources/{id}/muted-count", s.authed(s.mutedCount))
	mux.HandleFunc("POST /api/muted-count", s.authed(s.mutedCountAll))
	mux.HandleFunc("GET /api/rules", s.authed(s.listRules))
	mux.HandleFunc("POST /api/rules", s.authed(s.createRule))
	mux.HandleFunc("POST /api/rules/match-count", s.authed(s.ruleMatchCount))
	mux.HandleFunc("PATCH /api/rules/{id}", s.authed(s.updateRule))
	mux.HandleFunc("DELETE /api/rules/{id}", s.authed(s.deleteRule))
	mux.HandleFunc("POST /api/sources/refresh", s.authed(s.refreshAll))
	mux.HandleFunc("POST /api/sources/{id}/refresh", s.authed(s.refreshSource))
	mux.HandleFunc("GET /api/entries", s.authed(s.listEntries))
	mux.HandleFunc("GET /api/entries/{id}", s.authed(s.getEntry))
	mux.HandleFunc("PATCH /api/entries/{id}", s.authed(s.updateEntry))
	mux.HandleFunc("POST /api/entries/read", s.authed(s.markRead))
	mux.HandleFunc("POST /api/entries/unread", s.authed(s.markUnread))
	mux.HandleFunc("GET /api/entries/{id}/revision", s.authed(s.getRevision))
	mux.HandleFunc("GET /api/entries/{id}/ai", s.authed(s.getAI))
	mux.HandleFunc("POST /api/entries/{id}/extract", s.authed(s.retryExtraction))
	mux.HandleFunc("POST /api/entries/{id}/summary", s.authed(s.requestSummary))
	mux.HandleFunc("POST /api/entries/{id}/translation", s.authed(s.requestTranslation))
	mux.HandleFunc("POST /api/ai/test", s.authed(s.testAI))
	mux.HandleFunc("POST /api/ai/selection", s.authed(s.selectAI))
	mux.HandleFunc("GET /api/settings", s.authed(s.getSettings))
	mux.HandleFunc("PATCH /api/settings", s.authed(s.updateSettings))
	mux.HandleFunc("POST /api/opml/import", s.authed(s.importOPML))
	mux.HandleFunc("POST /api/opml/import/start", s.authed(s.startOPML))
	mux.HandleFunc("GET /api/opml/import", s.authed(s.opmlJobStatus))
	mux.HandleFunc("GET /api/opml/export", s.authed(s.exportOPML))
	mux.HandleFunc("GET /api/backup", s.authed(s.backup))
	mux.HandleFunc("POST /api/restore", s.authed(s.restore))
	mux.HandleFunc("POST /api/import/start", s.authed(s.startImport))
	mux.HandleFunc("GET /api/import", s.authed(s.importStatus))
	mux.HandleFunc("POST /api/import/confirm", s.authed(s.confirmImport))
	mux.HandleFunc("POST /api/import/cancel", s.authed(s.cancelImport))
	mux.HandleFunc("GET /api/image", s.authed(s.image))
	mux.HandleFunc("GET /api/activity", s.authed(s.activity))
	mux.HandleFunc("GET /api/rsshub/health", s.authed(s.rsshubHealth))
	mux.HandleFunc("GET /api/storage", s.authed(s.storage))
	mux.HandleFunc("POST /api/storage/vacuum", s.authed(s.startVacuum))
	mux.HandleFunc("DELETE /api/storage/premigrate", s.authed(s.deletePreMigrate))
	mux.HandleFunc("GET /api/diagnostics", s.authed(s.diagnostics))
	mux.HandleFunc("GET /api/discover", s.authed(s.findFeeds))
	mux.HandleFunc("GET /api/routes", s.authed(s.findRoutes))
	mux.HandleFunc("GET /api/routes/preview", s.authed(s.previewRoute))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { fail(w, http.StatusNotFound, "not_found") })
	s.handler = s.guard(mux)
	return s, nil
}

// guard turns away requests a browser page on another origin could make, and, without a password, requests that are
// not addressed to a loopback host. Clients that are not browsers send no Origin, so they pass. The WebSub callback is
// exempt from the host rule: hubs reach it through the public address the user forwards to this port.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.derived == nil && !strings.HasPrefix(r.URL.Path, "/websub/") && !loopbackHost(r.Host) {
			fail(w, http.StatusForbidden, "forbidden_host")
			return
		}
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				if err != nil || !strings.EqualFold(u.Host, r.Host) {
					fail(w, http.StatusForbidden, "forbidden_origin")
					return
				}
			}
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				fail(w, http.StatusForbidden, "forbidden_origin")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// loopbackHost reports whether a Host header, port removed, names this machine: localhost or a loopback address.
func loopbackHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.Trim(host, "[]"), ".")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback()
}

// jsonContent reports whether the request declares a JSON body; parameters such as charset are allowed.
func jsonContent(r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && mediaType == "application/json"
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
	if err := s.store.Ping(r.Context()); err != nil {
		slog.Error("Database cannot be read", "error", err)
		status, database = http.StatusServiceUnavailable, "unwritable"
	} else if err := s.probe.check(r.Context(), s.now, s.writable); err != nil {
		slog.Error("Database is not writable", "error", err)
		status, database = http.StatusServiceUnavailable, "unwritable"
	}
	reply(w, status, map[string]any{
		"app": "oriel", "version": s.version, "auth": s.derived != nil, "database": database,
	})
}

// probeEvery is how long a writability probe result is reused, so polling /health does not compete for the write lock.
const probeEvery = 10 * time.Second

// writeProbe shares one writability probe between all requests within probeEvery.
type writeProbe struct {
	mu      sync.Mutex
	at      time.Time
	err     error
	done    bool          // a probe has finished at least once
	running bool          // a probe is in flight
	wait    chan struct{} // closed when the probe in flight ends
}

// busy reports whether a probe failed only because another writer held the lock: that database is writable.
func busy(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	text := err.Error()
	return strings.Contains(text, "SQLITE_BUSY") || strings.Contains(text, "database is locked")
}

// check returns the result of the last probe while it is fresh. The first request after that runs a new probe,
// outside the lock. Requests that come meanwhile get the last result at once; with no result yet they wait for the
// probe, which ends within its own timeout, instead of answering for a database nobody has tested.
func (p *writeProbe) check(ctx context.Context, now func() time.Time, probe func(context.Context) error) error {
	p.mu.Lock()
	if p.done && now().Sub(p.at) < probeEvery {
		defer p.mu.Unlock()
		return p.err
	}
	if p.running {
		if p.done {
			defer p.mu.Unlock()
			return p.err
		}
		wait := p.wait
		p.mu.Unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return ctx.Err()
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.err
	}
	p.running, p.wait = true, make(chan struct{})
	wait := p.wait
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.running = false
		close(wait)
		p.mu.Unlock()
	}()
	// The probe waits at most a few seconds for the write lock: a busy writer is not an unwritable database.
	pctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err := probe(pctx)
	if err != nil && busy(err) {
		err = nil
	}
	p.mu.Lock()
	p.err, p.at, p.done = err, now(), true
	p.mu.Unlock()
	return err
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if s.derived == nil {
		fail(w, http.StatusBadRequest, "auth_disabled")
		return
	}
	key, now := clientKey(r, s.proxies), s.now()
	if wait := s.throttle.wait(key, now); wait > 0 {
		tooMany(w, wait)
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	// A malformed body, or one not declared as JSON, is treated as a wrong password, never as a field validation error.
	if !jsonContent(r) || json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body) != nil {
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
