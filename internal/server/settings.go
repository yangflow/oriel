package server

import (
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/yangflow/oriel/internal/ai"
	"github.com/yangflow/oriel/internal/fetch"
)

const (
	maxModel  = 200
	maxAPIKey = 4096
	maxPrompt = 4000
)

// settingsJSON lists every setting a client may read. Secrets are never listed, only whether they are set.
type settingsJSON struct {
	RSSHubBaseURL   string `json:"rsshubBaseUrl"`
	RetentionDays   int    `json:"retentionDays"`
	AIBaseURL       string `json:"aiBaseUrl"`
	AIModel         string `json:"aiModel"`
	AIAPIKeySet     bool   `json:"aiApiKeySet"`
	AILanguage      string `json:"aiLanguage"`
	PromptSummary   string `json:"promptSummary"`
	PromptTranslate string `json:"promptTranslate"`
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	root, err := s.store.Setting(r.Context(), fetch.RSSHubSetting)
	if err != nil {
		internal(w, err)
		return
	}
	raw, err := s.store.Setting(r.Context(), fetch.RetentionSetting)
	if err != nil {
		internal(w, err)
		return
	}
	// The fetch loop treats a value that is not a day count as forever, and so does the reply.
	days, err := strconv.Atoi(raw)
	if err != nil || days < 0 || days > fetch.MaxRetentionDays {
		days = 0
	}
	cfg, err := ai.Load(r.Context(), s.store)
	if err != nil {
		internal(w, err)
		return
	}
	reply(w, http.StatusOK, settingsJSON{
		RSSHubBaseURL: root, RetentionDays: days, AIBaseURL: cfg.BaseURL, AIModel: cfg.Model,
		AIAPIKeySet: cfg.APIKey != "", AILanguage: cfg.Language, PromptSummary: cfg.PromptSummary,
		PromptTranslate: cfg.PromptTranslate,
	})
}

func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RSSHubBaseURL   *string `json:"rsshubBaseUrl"`
		RetentionDays   *int    `json:"retentionDays"`
		AIBaseURL       *string `json:"aiBaseUrl"`
		AIModel         *string `json:"aiModel"`
		AIAPIKey        *string `json:"aiApiKey"`
		AILanguage      *string `json:"aiLanguage"`
		PromptSummary   *string `json:"promptSummary"`
		PromptTranslate *string `json:"promptTranslate"`
	}
	if !readBody(w, r, &body) {
		return
	}
	var changes [][2]string
	if body.RSSHubBaseURL != nil {
		root := strings.TrimSpace(*body.RSSHubBaseURL)
		if root != "" && !feedURL(root) {
			fail(w, http.StatusBadRequest, "invalid_rsshub_base_url")
			return
		}
		changes = append(changes, [2]string{fetch.RSSHubSetting, root})
	}
	if body.RetentionDays != nil {
		days := *body.RetentionDays
		if days < 0 || days > fetch.MaxRetentionDays {
			fail(w, http.StatusBadRequest, "invalid_retention_days")
			return
		}
		changes = append(changes, [2]string{fetch.RetentionSetting, strconv.Itoa(days)})
	}
	if body.AIBaseURL != nil {
		root := strings.TrimSpace(*body.AIBaseURL)
		if root != "" && !feedURL(root) {
			fail(w, http.StatusBadRequest, "invalid_ai_base_url")
			return
		}
		changes = append(changes, [2]string{ai.BaseURLSetting, strings.TrimRight(root, "/")})
	}
	for _, field := range []struct {
		value *string
		key   string
		limit int
		code  string
	}{
		{body.AIModel, ai.ModelSetting, maxModel, "invalid_ai_model"},
		{body.AIAPIKey, ai.APIKeySetting, maxAPIKey, "invalid_ai_api_key"},
		{body.PromptSummary, ai.PromptSummarySetting, maxPrompt, "invalid_prompt"},
		{body.PromptTranslate, ai.PromptTranslateSetting, maxPrompt, "invalid_prompt"},
	} {
		if field.value == nil {
			continue
		}
		value := strings.TrimSpace(*field.value)
		if utf8.RuneCountInString(value) > field.limit {
			fail(w, http.StatusBadRequest, field.code)
			return
		}
		changes = append(changes, [2]string{field.key, value})
	}
	if body.AILanguage != nil {
		if _, ok := ai.Languages[*body.AILanguage]; !ok {
			fail(w, http.StatusBadRequest, "invalid_ai_language")
			return
		}
		changes = append(changes, [2]string{ai.LanguageSetting, *body.AILanguage})
	}
	for _, change := range changes {
		if err := s.store.SetSetting(r.Context(), change[0], change[1]); err != nil {
			internal(w, err)
			return
		}
	}
	s.getSettings(w, r)
}
