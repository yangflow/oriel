package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/yangflow/oriel/internal/fetch"
	"github.com/yangflow/oriel/internal/store"
)

const (
	maxBody         = 64 << 10
	defaultInterval = 900 * time.Second
	minInterval     = time.Minute
	maxInterval     = 7 * 24 * time.Hour
)

var kinds = map[string]bool{"article": true, "video": true, "social": true, "image": true}

type sourceJSON struct {
	ID                  int64  `json:"id"`
	URL                 string `json:"url"`
	RSSHubPath          string `json:"rsshubPath"`
	Title               string `json:"title"`
	SiteURL             string `json:"siteUrl"`
	IconURL             string `json:"iconUrl"`
	Kind                string `json:"kind"`
	FetchIntervalSec    int64  `json:"fetchIntervalSec"`
	FetchIntervalAuto   bool   `json:"fetchIntervalAuto"`
	SuggestedKind       string `json:"suggestedKind"`
	NextFetchAt         int64  `json:"nextFetchAt"`
	LastFetchedAt       *int64 `json:"lastFetchedAt"`
	LastSuccessAt       *int64 `json:"lastSuccessAt"`
	LastError           string `json:"lastError"`
	LastErrorKind       string `json:"lastErrorKind"`
	ConsecutiveFailures int    `json:"consecutiveFailures"`
	PromptSummary       string `json:"promptSummary"`
	PromptTranslate     string `json:"promptTranslate"`
	AutoSummary         bool   `json:"autoSummary"`
	AutoTranslate       bool   `json:"autoTranslate"`
	MuteKeywords        string `json:"muteKeywords"`
	Paused              bool   `json:"paused"`
	KeepDays            int    `json:"keepDays"`
	PaceSec             int    `json:"paceSec"`
	ExtractSelector     string `json:"extractSelector"`
	ExtractReferer      string `json:"extractReferer"`
	ExtractRewrites     string `json:"extractRewrites"`
	RSSHubInstance      string `json:"rsshubInstance"`
	Proxy               string `json:"proxy"`
	ProxyAuthSet        bool   `json:"proxyAuthSet"`
	WebSubHub           string `json:"websubHub"`
	WebSubActive        bool   `json:"websubActive"`
	UnreadCount         int    `json:"unreadCount"`
	CreatedAt           int64  `json:"createdAt"`
	UpdatedAt           int64  `json:"updatedAt"`
}

func toJSON(src store.Source) sourceJSON {
	suggested := src.SuggestedKind
	if suggested == src.Kind {
		suggested = ""
	}
	return sourceJSON{
		FetchIntervalAuto: src.FetchIntervalAuto, SuggestedKind: suggested,
		ID: src.ID, URL: src.URL, RSSHubPath: src.RSSHubPath, Title: src.Title, SiteURL: src.SiteURL,
		IconURL: src.IconURL, Kind: src.Kind, FetchIntervalSec: int64(src.FetchInterval / time.Second),
		NextFetchAt: src.NextFetchAt.UnixMilli(), LastFetchedAt: millis(src.LastFetchedAt),
		LastSuccessAt: millis(src.LastSuccessAt), LastError: src.LastError, LastErrorKind: src.LastErrorKind,
		ConsecutiveFailures: src.ConsecutiveFailures, PromptSummary: src.PromptSummary,
		PromptTranslate: src.PromptTranslate, AutoSummary: src.AutoSummary, AutoTranslate: src.AutoTranslate,
		MuteKeywords: src.MuteKeywords, Paused: src.Paused, KeepDays: src.KeepDays, PaceSec: src.PaceSec, ExtractSelector: src.ExtractSelector, ExtractReferer: src.ExtractReferer, ExtractRewrites: src.ExtractRewrites, RSSHubInstance: src.RSSHubInstance, Proxy: src.Proxy, ProxyAuthSet: src.ProxyAuthSet, WebSubHub: src.WebSubHub,
		WebSubActive: src.WebSubUntil.After(time.Now()), UnreadCount: src.UnreadCount,
		CreatedAt: src.CreatedAt.UnixMilli(), UpdatedAt: src.UpdatedAt.UnixMilli(),
	}
}

// readBody decodes a JSON body of at most maxBody bytes with no unknown fields, or replies unsupported_media_type
// when the request does not declare JSON, or invalid_body.
func readBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if !jsonContent(r) {
		fail(w, http.StatusUnsupportedMediaType, "unsupported_media_type")
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil || decoder.More() {
		fail(w, http.StatusBadRequest, "invalid_body")
		return false
	}
	return true
}

// pathID reads the {id} of the route, or replies invalid_id.
func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fail(w, http.StatusBadRequest, "invalid_id")
		return 0, false
	}
	return id, true
}

// interval validates a fetch interval in seconds and replies invalid_interval when it is out of range.
func interval(w http.ResponseWriter, seconds int64) (time.Duration, bool) {
	if seconds < int64(minInterval/time.Second) || seconds > int64(maxInterval/time.Second) {
		fail(w, http.StatusBadRequest, "invalid_interval")
		return 0, false
	}
	return time.Duration(seconds) * time.Second, true
}

// muteLines trims every line and drops the empty ones.
func muteLines(text string) string {
	return store.CleanLines(text)
}

func (s *Server) listSources(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.Sources(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	items := make([]sourceJSON, len(list))
	for i, src := range list {
		items[i] = toJSON(src)
	}
	group, err := s.store.GroupSameLink(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	total, err := s.store.UnreadTotal(r.Context(), group)
	if err != nil {
		internal(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"sources": items, "unreadTotal": total})
}

type healthJSON struct {
	ID                  int64  `json:"id"`
	URL                 string `json:"url"`
	RSSHubPath          string `json:"rsshubPath"`
	Title               string `json:"title"`
	IconURL             string `json:"iconUrl"`
	Kind                string `json:"kind"`
	State               string `json:"state"`
	Cause               string `json:"cause"`
	Days                int    `json:"days"`
	ConsecutiveFailures int    `json:"consecutiveFailures"`
	LastError           string `json:"lastError"`
	Recent              int    `json:"recent"`
	RecentRead          int    `json:"recentRead"`
	RecentStarred       int    `json:"recentStarred"`
}

// sourcesHealth lists the sources that are dead, failing or rarely read (docs/product.md, "订阅健康").
func (s *Server) sourcesHealth(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.SourcesHealth(r.Context(), s.now())
	if err != nil {
		internal(w, err)
		return
	}
	items := make([]healthJSON, len(list))
	for i, h := range list {
		items[i] = healthJSON(h)
	}
	reply(w, http.StatusOK, map[string]any{"windowDays": store.HealthWindowDays, "sources": items})
}

func (s *Server) createSource(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL               string `json:"url"`
		RSSHubPath        string `json:"rsshubPath"`
		Kind              string `json:"kind"`
		Title             string `json:"title"`
		FetchIntervalSec  *int64 `json:"fetchIntervalSec"`
		FetchIntervalAuto *bool  `json:"fetchIntervalAuto"`
		AllowDuplicate    bool   `json:"allowDuplicate"`
	}
	if !readBody(w, r, &body) {
		return
	}
	src := store.NewSource{
		URL: strings.TrimSpace(body.URL), RSSHubPath: strings.TrimSpace(body.RSSHubPath), Kind: body.Kind,
		FetchInterval: defaultInterval, FetchIntervalAuto: true, AllowDuplicate: body.AllowDuplicate,
	}
	if code := checkSource(src); code != "" {
		fail(w, http.StatusBadRequest, code)
		return
	}
	if body.FetchIntervalSec != nil {
		if body.FetchIntervalAuto != nil && *body.FetchIntervalAuto {
			fail(w, http.StatusBadRequest, "invalid_interval")
			return
		}
		d, ok := interval(w, *body.FetchIntervalSec)
		if !ok {
			return
		}
		src.FetchInterval, src.FetchIntervalAuto = d, false
	} else if body.FetchIntervalAuto != nil {
		src.FetchIntervalAuto = *body.FetchIntervalAuto
	}
	created, err := s.store.CreateSource(r.Context(), src, strings.TrimSpace(body.Title), s.now())
	var exists *store.ExistsError
	switch {
	case errors.As(err, &exists):
		reply(w, http.StatusConflict, map[string]any{"error": "source_exists", "sourceId": exists.ID})
	case errors.Is(err, store.ErrExists):
		fail(w, http.StatusConflict, "source_exists")
	case err != nil:
		internal(w, err)
	default:
		reply(w, http.StatusCreated, toJSON(created))
	}
}

// checkSource returns the error code for a new subscription with trimmed addresses, or "" when it is valid.
func checkSource(src store.NewSource) string {
	switch {
	case (src.URL == "") == (src.RSSHubPath == ""):
		return "invalid_source"
	case src.URL != "" && !feedURL(src.URL):
		return "invalid_url"
	case src.RSSHubPath != "" && (!strings.HasPrefix(src.RSSHubPath, "/") || strings.HasPrefix(src.RSSHubPath, "//")):
		return "invalid_rsshub_path"
	case !kinds[src.Kind]:
		return "invalid_kind"
	}
	return ""
}

// feedURL accepts an absolute http or https address with a host name.
func feedURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != ""
}

func (s *Server) getSource(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	src, err := s.store.Source(r.Context(), id)
	replySource(w, src, err)
}

func (s *Server) updateSource(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		Title             *string `json:"title"`
		Kind              *string `json:"kind"`
		FetchIntervalSec  *int64  `json:"fetchIntervalSec"`
		FetchIntervalAuto *bool   `json:"fetchIntervalAuto"`
		PromptSummary     *string `json:"promptSummary"`
		PromptTranslate   *string `json:"promptTranslate"`
		AutoSummary       *bool   `json:"autoSummary"`
		AutoTranslate     *bool   `json:"autoTranslate"`
		MuteKeywords      *string `json:"muteKeywords"`
		Paused            *bool   `json:"paused"`
		KeepDays          *int    `json:"keepDays"`
		PaceSec           *int    `json:"paceSec"`
		ExtractSelector   *string `json:"extractSelector"`
		ExtractReferer    *string `json:"extractReferer"`
		ExtractRewrites   *string `json:"extractRewrites"`
		Proxy             *string `json:"proxy"`
		ProxyUser         *string `json:"proxyUser"`
		ProxyPassword     *string `json:"proxyPassword"`
	}
	if !readBody(w, r, &body) {
		return
	}
	patch := store.SourcePatch{
		Kind: body.Kind, PromptSummary: body.PromptSummary, PromptTranslate: body.PromptTranslate,
		AutoSummary: body.AutoSummary, AutoTranslate: body.AutoTranslate, Paused: body.Paused,
	}
	if body.Title != nil {
		title := strings.TrimSpace(*body.Title)
		patch.Title = &title
	}
	if body.Kind != nil && !kinds[*body.Kind] {
		fail(w, http.StatusBadRequest, "invalid_kind")
		return
	}
	patch.FetchIntervalAuto = body.FetchIntervalAuto
	if body.FetchIntervalSec != nil {
		if body.FetchIntervalAuto != nil && *body.FetchIntervalAuto {
			fail(w, http.StatusBadRequest, "invalid_interval")
			return
		}
		d, ok := interval(w, *body.FetchIntervalSec)
		if !ok {
			return
		}
		fixed := false
		patch.FetchInterval, patch.FetchIntervalAuto = &d, &fixed
	}
	if body.KeepDays != nil {
		if days := *body.KeepDays; days < -1 || days > fetch.MaxRetentionDays {
			fail(w, http.StatusBadRequest, "invalid_keep_days")
			return
		}
		patch.KeepDays = body.KeepDays
	}
	if body.PaceSec != nil {
		if sec := *body.PaceSec; sec != 0 && (sec < minPaceSec || sec > maxPaceSec) {
			fail(w, http.StatusBadRequest, "invalid_pace")
			return
		}
		patch.PaceSec = body.PaceSec
	}
	if body.ExtractSelector != nil {
		selector, err := fetch.ParseSelector(*body.ExtractSelector)
		if err != nil {
			fail(w, http.StatusBadRequest, "invalid_extract_selector")
			return
		}
		patch.ExtractSelector = &selector
	}
	if body.ExtractReferer != nil {
		referer, err := fetch.ParseReferer(*body.ExtractReferer)
		if err != nil {
			fail(w, http.StatusBadRequest, "invalid_extract_referer")
			return
		}
		patch.ExtractReferer = &referer
	}
	if body.ExtractRewrites != nil {
		rewrites, err := fetch.ParseRewrites(*body.ExtractRewrites)
		if err != nil {
			fail(w, http.StatusBadRequest, "invalid_extract_rewrites")
			return
		}
		patch.ExtractRewrites = &rewrites
	}
	if body.Proxy != nil {
		address := strings.TrimSpace(*body.Proxy)
		if address != "" && address != store.ProxyDirect && fetch.ValidProxyURL(address) != nil {
			fail(w, http.StatusBadRequest, "invalid_proxy")
			return
		}
		patch.Proxy = &address
	}
	for _, field := range []struct {
		value *string
		dst   **string
	}{{body.ProxyUser, &patch.ProxyUser}, {body.ProxyPassword, &patch.ProxyPassword}} {
		if field.value == nil {
			continue
		}
		if len(*field.value) > fetch.MaxProxyCredential {
			fail(w, http.StatusBadRequest, "invalid_proxy")
			return
		}
		*field.dst = field.value
	}
	if body.MuteKeywords != nil {
		keywords := muteLines(*body.MuteKeywords)
		patch.MuteKeywords = &keywords
	}
	src, err := s.store.UpdateSource(r.Context(), id, patch, s.now())
	replySource(w, src, err)
}

// minPaceSec and maxPaceSec bound a source's pace in seconds: an hour to 30 days; 0 turns it off.
const (
	minPaceSec = 3600
	maxPaceSec = 30 * 24 * 3600
)

// mutedCount says how many entries of a source the given keywords would hide, without saving them.
func (s *Server) mutedCount(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		MuteKeywords string `json:"muteKeywords"`
	}
	if !readBody(w, r, &body) {
		return
	}
	n, err := s.store.MutedCount(r.Context(), id, muteLines(body.MuteKeywords))
	switch {
	case errors.Is(err, store.ErrNotFound):
		fail(w, http.StatusNotFound, "not_found")
	case err != nil:
		internal(w, err)
	default:
		reply(w, http.StatusOK, map[string]int{"count": n})
	}
}

// mutedCountAll says how many entries the given global keywords would hide, without saving them.
func (s *Server) mutedCountAll(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MuteKeywords string `json:"muteKeywords"`
	}
	if !readBody(w, r, &body) {
		return
	}
	keywords := muteLines(body.MuteKeywords)
	if !store.LinesWithin(keywords, store.MaxMuteKeywords) {
		fail(w, http.StatusBadRequest, "invalid_mute_keywords")
		return
	}
	n, err := s.store.MutedCountAll(r.Context(), keywords)
	if err != nil {
		internal(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]int{"count": n})
}

func replySource(w http.ResponseWriter, src store.Source, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		fail(w, http.StatusNotFound, "not_found")
	case err != nil:
		internal(w, err)
	default:
		reply(w, http.StatusOK, toJSON(src))
	}
}

func (s *Server) deleteSource(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	switch err := s.store.DeleteSource(r.Context(), id, s.now()); {
	case errors.Is(err, store.ErrNotFound):
		fail(w, http.StatusNotFound, "not_found")
	case err != nil:
		internal(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// restoreSource answers POST /api/sources/{id}/restore: it undoes a delete within store.DeleteUndoWindow.
func (s *Server) restoreSource(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	src, err := s.store.RestoreSource(r.Context(), id, s.now())
	replySource(w, src, err)
}

func (s *Server) refreshSource(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	switch err := s.store.MarkSourceDue(r.Context(), id, s.now()); {
	case errors.Is(err, store.ErrNotFound):
		fail(w, http.StatusNotFound, "not_found")
	case errors.Is(err, store.ErrPaused):
		fail(w, http.StatusConflict, "source_paused")
	case err != nil:
		internal(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// refreshAll makes every unpaused subscription due now; the fetch loop fetches them with its own workers and per-host
// limits. at is the time they were made due, which a client compares with lastFetchedAt to count the ones checked since.
func (s *Server) refreshAll(w http.ResponseWriter, r *http.Request) {
	now := s.now()
	n, err := s.store.MarkAllSourcesDue(r.Context(), now)
	if err != nil {
		internal(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"queued": n, "at": now.UnixMilli()})
}
