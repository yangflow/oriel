package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

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
	NextFetchAt         int64  `json:"nextFetchAt"`
	LastFetchedAt       *int64 `json:"lastFetchedAt"`
	LastSuccessAt       *int64 `json:"lastSuccessAt"`
	LastError           string `json:"lastError"`
	ConsecutiveFailures int    `json:"consecutiveFailures"`
	PromptSummary       string `json:"promptSummary"`
	PromptTranslate     string `json:"promptTranslate"`
	AutoSummary         bool   `json:"autoSummary"`
	AutoTranslate       bool   `json:"autoTranslate"`
	MuteKeywords        string `json:"muteKeywords"`
	Paused              bool   `json:"paused"`
	UnreadCount         int    `json:"unreadCount"`
	CreatedAt           int64  `json:"createdAt"`
	UpdatedAt           int64  `json:"updatedAt"`
}

func toJSON(src store.Source) sourceJSON {
	return sourceJSON{
		ID: src.ID, URL: src.URL, RSSHubPath: src.RSSHubPath, Title: src.Title, SiteURL: src.SiteURL,
		IconURL: src.IconURL, Kind: src.Kind, FetchIntervalSec: int64(src.FetchInterval / time.Second),
		NextFetchAt: src.NextFetchAt.UnixMilli(), LastFetchedAt: millis(src.LastFetchedAt),
		LastSuccessAt: millis(src.LastSuccessAt), LastError: src.LastError,
		ConsecutiveFailures: src.ConsecutiveFailures, PromptSummary: src.PromptSummary,
		PromptTranslate: src.PromptTranslate, AutoSummary: src.AutoSummary, AutoTranslate: src.AutoTranslate,
		MuteKeywords: src.MuteKeywords, Paused: src.Paused, UnreadCount: src.UnreadCount,
		CreatedAt: src.CreatedAt.UnixMilli(), UpdatedAt: src.UpdatedAt.UnixMilli(),
	}
}

// readBody decodes a JSON body of at most maxBody bytes with no unknown fields, or replies invalid_body.
func readBody(w http.ResponseWriter, r *http.Request, v any) bool {
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
	var lines []string
	for line := range strings.Lines(text) {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
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
	reply(w, http.StatusOK, map[string]any{"sources": items})
}

func (s *Server) createSource(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL              string `json:"url"`
		RSSHubPath       string `json:"rsshubPath"`
		Kind             string `json:"kind"`
		Title            string `json:"title"`
		FetchIntervalSec *int64 `json:"fetchIntervalSec"`
	}
	if !readBody(w, r, &body) {
		return
	}
	src := store.NewSource{
		URL: strings.TrimSpace(body.URL), RSSHubPath: strings.TrimSpace(body.RSSHubPath), Kind: body.Kind,
		FetchInterval: defaultInterval,
	}
	switch {
	case (src.URL == "") == (src.RSSHubPath == ""):
		fail(w, http.StatusBadRequest, "invalid_source")
		return
	case src.URL != "" && !feedURL(src.URL):
		fail(w, http.StatusBadRequest, "invalid_url")
		return
	case src.RSSHubPath != "" && (!strings.HasPrefix(src.RSSHubPath, "/") || strings.HasPrefix(src.RSSHubPath, "//")):
		fail(w, http.StatusBadRequest, "invalid_rsshub_path")
		return
	case !kinds[src.Kind]:
		fail(w, http.StatusBadRequest, "invalid_kind")
		return
	}
	if body.FetchIntervalSec != nil {
		d, ok := interval(w, *body.FetchIntervalSec)
		if !ok {
			return
		}
		src.FetchInterval = d
	}
	created, err := s.store.CreateSource(r.Context(), src, strings.TrimSpace(body.Title), s.now())
	switch {
	case errors.Is(err, store.ErrExists):
		fail(w, http.StatusConflict, "source_exists")
	case err != nil:
		internal(w, err)
	default:
		reply(w, http.StatusCreated, toJSON(created))
	}
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
		Title            *string `json:"title"`
		Kind             *string `json:"kind"`
		FetchIntervalSec *int64  `json:"fetchIntervalSec"`
		PromptSummary    *string `json:"promptSummary"`
		PromptTranslate  *string `json:"promptTranslate"`
		AutoSummary      *bool   `json:"autoSummary"`
		AutoTranslate    *bool   `json:"autoTranslate"`
		MuteKeywords     *string `json:"muteKeywords"`
		Paused           *bool   `json:"paused"`
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
	if body.FetchIntervalSec != nil {
		d, ok := interval(w, *body.FetchIntervalSec)
		if !ok {
			return
		}
		patch.FetchInterval = &d
	}
	if body.MuteKeywords != nil {
		keywords := muteLines(*body.MuteKeywords)
		patch.MuteKeywords = &keywords
	}
	src, err := s.store.UpdateSource(r.Context(), id, patch, s.now())
	replySource(w, src, err)
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
	switch err := s.store.DeleteSource(r.Context(), id); {
	case errors.Is(err, store.ErrNotFound):
		fail(w, http.StatusNotFound, "not_found")
	case err != nil:
		internal(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
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
