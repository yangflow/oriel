package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/yangflow/oriel/internal/store"
)

type ruleJSON struct {
	ID        int64  `json:"id"`
	SourceID  *int64 `json:"sourceId"`
	Kind      string `json:"kind"`
	Keywords  string `json:"keywords"`
	Author    string `json:"author"`
	Star      bool   `json:"star"`
	MarkRead  bool   `json:"markRead"`
	Enabled   bool   `json:"enabled"`
	CreatedAt int64  `json:"createdAt"`
}

func toRuleJSON(r store.Rule) ruleJSON {
	return ruleJSON{ID: r.ID, SourceID: r.SourceID, Kind: r.Kind, Keywords: r.Keywords, Author: r.Author,
		Star: r.Star, MarkRead: r.MarkRead, Enabled: r.Enabled, CreatedAt: r.CreatedAt.UnixMilli()}
}

// replyRuleError answers a rule the store did not accept, or reports the failure; it returns false when err is nil.
func replyRuleError(w http.ResponseWriter, err error) bool {
	codes := []struct {
		err    error
		status int
		code   string
	}{
		{store.ErrNotFound, http.StatusNotFound, "not_found"},
		{store.ErrRuleSource, http.StatusBadRequest, "invalid_rule_source"},
		{store.ErrRuleKind, http.StatusBadRequest, "invalid_rule_kind"},
		{store.ErrRuleKeywords, http.StatusBadRequest, "invalid_rule_keywords"},
		{store.ErrRuleAuthor, http.StatusBadRequest, "invalid_rule_author"},
		{store.ErrRuleNoCondition, http.StatusBadRequest, "rule_no_condition"},
		{store.ErrRuleNoAction, http.StatusBadRequest, "rule_no_action"},
		{store.ErrTooManyRules, http.StatusConflict, "too_many_rules"},
	}
	if err == nil {
		return false
	}
	for _, c := range codes {
		if errors.Is(err, c.err) {
			fail(w, c.status, c.code)
			return true
		}
	}
	internal(w, err)
	return true
}

func (s *Server) listRules(w http.ResponseWriter, r *http.Request) {
	rules, err := s.store.Rules(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	list := make([]ruleJSON, 0, len(rules))
	for _, rule := range rules {
		list = append(list, toRuleJSON(rule))
	}
	reply(w, http.StatusOK, map[string]any{"rules": list})
}

func (s *Server) createRule(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID        int64  `json:"id"`        // a restored rule asks for its old place
		CreatedAt int64  `json:"createdAt"` // in milliseconds
		SourceID  *int64 `json:"sourceId"`
		Kind      string `json:"kind"`
		Keywords  string `json:"keywords"`
		Author    string `json:"author"`
		Star      bool   `json:"star"`
		MarkRead  bool   `json:"markRead"`
		Enabled   *bool  `json:"enabled"`
	}
	if !readBody(w, r, &body) {
		return
	}
	rule := store.Rule{ID: body.ID, SourceID: body.SourceID, Kind: body.Kind, Keywords: body.Keywords, Author: body.Author,
		Star: body.Star, MarkRead: body.MarkRead, Enabled: body.Enabled == nil || *body.Enabled}
	at := s.now()
	if body.ID > 0 && body.CreatedAt > 0 && body.CreatedAt < at.UnixMilli() {
		at = time.UnixMilli(body.CreatedAt)
	}
	if body.ID <= 0 {
		rule.ID = 0
	}
	created, err := s.store.CreateRule(r.Context(), rule, at)
	if replyRuleError(w, err) {
		return
	}
	reply(w, http.StatusCreated, toRuleJSON(created))
}

func (s *Server) updateRule(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		SourceID json.RawMessage `json:"sourceId"` // absent leaves the source, null is every source
		Kind     *string         `json:"kind"`
		Keywords *string         `json:"keywords"`
		Author   *string         `json:"author"`
		Star     *bool           `json:"star"`
		MarkRead *bool           `json:"markRead"`
		Enabled  *bool           `json:"enabled"`
	}
	if !readBody(w, r, &body) {
		return
	}
	patch := store.RulePatch{Kind: body.Kind, Keywords: body.Keywords, Author: body.Author,
		Star: body.Star, MarkRead: body.MarkRead, Enabled: body.Enabled}
	if body.SourceID != nil {
		patch.SetSource = true
		if err := json.Unmarshal(body.SourceID, &patch.SourceID); err != nil {
			fail(w, http.StatusBadRequest, "invalid_body")
			return
		}
	}
	updated, err := s.store.UpdateRule(r.Context(), id, patch)
	if replyRuleError(w, err) {
		return
	}
	reply(w, http.StatusOK, toRuleJSON(updated))
}

func (s *Server) deleteRule(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if replyRuleError(w, s.store.DeleteRule(r.Context(), id)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
