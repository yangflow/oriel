package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/yangflow/oriel/internal/store"
)

type ruleJSON struct {
	ID              int64   `json:"id"`
	SourceIDs       []int64 `json:"sourceIds"` // none is every subscription
	Kind            string  `json:"kind"`
	Keywords        string  `json:"keywords"`
	ExcludeKeywords string  `json:"excludeKeywords"`
	Author          string  `json:"author"`
	Star            bool    `json:"star"`
	MarkRead        bool    `json:"markRead"`
	Enabled         bool    `json:"enabled"`
	CreatedAt       int64   `json:"createdAt"`
}

func toRuleJSON(r store.Rule) ruleJSON {
	ids := r.SourceIDs
	if ids == nil {
		ids = []int64{}
	}
	return ruleJSON{ID: r.ID, SourceIDs: ids, Kind: r.Kind, Keywords: r.Keywords, ExcludeKeywords: r.Exclude, Author: r.Author,
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
		{store.ErrRuleSources, http.StatusBadRequest, "too_many_rule_sources"},
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

// ruleSources reads the subscriptions of a rule body: sourceIds (null or [] is every subscription), or, only when that
// is absent, the single sourceId older clients send (null is every subscription). It returns nil when neither is given.
func ruleSources(ids, legacy json.RawMessage) (*[]int64, bool) {
	list := []int64{}
	switch {
	case ids != nil:
		if err := json.Unmarshal(ids, &list); err != nil {
			return nil, false
		}
		if list == nil {
			list = []int64{}
		}
	case legacy != nil:
		var one *int64
		if err := json.Unmarshal(legacy, &one); err != nil {
			return nil, false
		}
		if one != nil {
			list = append(list, *one)
		}
	default:
		return nil, true
	}
	return &list, true
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
		ID        int64           `json:"id"`        // a restored rule asks for its old place
		CreatedAt int64           `json:"createdAt"` // in milliseconds
		SourceIDs json.RawMessage `json:"sourceIds"`
		SourceID  json.RawMessage `json:"sourceId"` // the one subscription older clients send; used when sourceIds is absent
		Kind      string          `json:"kind"`
		Keywords  string          `json:"keywords"`
		Exclude   string          `json:"excludeKeywords"`
		Author    string          `json:"author"`
		Star      bool            `json:"star"`
		MarkRead  bool            `json:"markRead"`
		Enabled   *bool           `json:"enabled"`
	}
	if !readBody(w, r, &body) {
		return
	}
	ids, ok := ruleSources(body.SourceIDs, body.SourceID)
	if !ok {
		fail(w, http.StatusBadRequest, "invalid_body")
		return
	}
	rule := store.Rule{ID: body.ID, Kind: body.Kind, Keywords: body.Keywords, Exclude: body.Exclude, Author: body.Author,
		Star: body.Star, MarkRead: body.MarkRead, Enabled: body.Enabled == nil || *body.Enabled}
	if ids != nil {
		rule.SourceIDs = *ids
	}
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
		SourceIDs json.RawMessage `json:"sourceIds"` // absent leaves the subscriptions, null or [] is every subscription
		SourceID  json.RawMessage `json:"sourceId"`  // the old single form, used when sourceIds is absent
		Kind      *string         `json:"kind"`
		Keywords  *string         `json:"keywords"`
		Exclude   *string         `json:"excludeKeywords"`
		Author    *string         `json:"author"`
		Star      *bool           `json:"star"`
		MarkRead  *bool           `json:"markRead"`
		Enabled   *bool           `json:"enabled"`
	}
	if !readBody(w, r, &body) {
		return
	}
	ids, ok := ruleSources(body.SourceIDs, body.SourceID)
	if !ok {
		fail(w, http.StatusBadRequest, "invalid_body")
		return
	}
	patch := store.RulePatch{SourceIDs: ids, Kind: body.Kind, Keywords: body.Keywords, Exclude: body.Exclude, Author: body.Author,
		Star: body.Star, MarkRead: body.MarkRead, Enabled: body.Enabled}
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

// ruleMatchCount says how many existing entries the conditions in the body would match, without saving a rule.
func (s *Server) ruleMatchCount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SourceIDs json.RawMessage `json:"sourceIds"`
		Kind      string          `json:"kind"`
		Keywords  string          `json:"keywords"`
		Exclude   string          `json:"excludeKeywords"`
		Author    string          `json:"author"`
	}
	if !readBody(w, r, &body) {
		return
	}
	ids, ok := ruleSources(body.SourceIDs, nil)
	if !ok {
		fail(w, http.StatusBadRequest, "invalid_body")
		return
	}
	rule := store.Rule{Kind: body.Kind, Keywords: body.Keywords, Exclude: body.Exclude, Author: body.Author}
	if ids != nil {
		rule.SourceIDs = *ids
	}
	n, err := s.store.RuleMatchCount(r.Context(), rule)
	if replyRuleError(w, err) {
		return
	}
	reply(w, http.StatusOK, map[string]int{"count": n})
}
