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
	// The RSSHub instances in priority order; RSSHubBaseURL is the first, for clients that know one.
	RSSHubBaseURLs  []string `json:"rsshubBaseUrls"`
	RSSHubBaseURL   string   `json:"rsshubBaseUrl"`
	RetentionDays   int      `json:"retentionDays"`
	AIBaseURL       string   `json:"aiBaseUrl"`
	AIModel         string   `json:"aiModel"`
	AIAPIKeySet     bool     `json:"aiApiKeySet"`
	AILanguage      string   `json:"aiLanguage"`
	PromptSummary   string   `json:"promptSummary"`
	PromptTranslate string   `json:"promptTranslate"`
	// The built-in prompts, so a client can tell a changed prompt and restore it.
	DefaultPromptSummary   string `json:"defaultPromptSummary"`
	DefaultPromptTranslate string `json:"defaultPromptTranslate"`
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
	instances := fetch.Instances(root)
	if instances == nil {
		instances = []string{}
	}
	primary := ""
	if len(instances) > 0 {
		primary = instances[0]
	}
	reply(w, http.StatusOK, settingsJSON{
		RSSHubBaseURLs: instances, RSSHubBaseURL: primary, RetentionDays: days, AIBaseURL: cfg.BaseURL, AIModel: cfg.Model,
		AIAPIKeySet: cfg.APIKey != "", AILanguage: cfg.Language, PromptSummary: cfg.PromptSummary,
		PromptTranslate: cfg.PromptTranslate, DefaultPromptSummary: ai.DefaultPromptSummary,
		DefaultPromptTranslate: ai.DefaultPromptTranslate,
	})
}

func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RSSHubBaseURLs  *[]string `json:"rsshubBaseUrls"`
		RSSHubBaseURL   *string   `json:"rsshubBaseUrl"`
		RetentionDays   *int      `json:"retentionDays"`
		AIBaseURL       *string   `json:"aiBaseUrl"`
		AIModel         *string   `json:"aiModel"`
		AIAPIKey        *string   `json:"aiApiKey"`
		AILanguage      *string   `json:"aiLanguage"`
		PromptSummary   *string   `json:"promptSummary"`
		PromptTranslate *string   `json:"promptTranslate"`
	}
	if !readBody(w, r, &body) {
		return
	}
	var changes [][2]string
	// The list wins over the single address older clients send.
	var roots *[]string
	switch {
	case body.RSSHubBaseURLs != nil:
		roots = body.RSSHubBaseURLs
	case body.RSSHubBaseURL != nil:
		roots = &[]string{*body.RSSHubBaseURL}
	}
	if roots != nil {
		stored, code := rsshubInstances(*roots)
		if code != "" {
			fail(w, http.StatusBadRequest, code)
			return
		}
		changes = append(changes, [2]string{fetch.RSSHubSetting, stored})
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

// rsshubInstances validates RSSHub root addresses and returns the setting value that holds them, one per line, or
// the error code. Blank entries are ignored and a repeated address is kept once.
func rsshubInstances(roots []string) (string, string) {
	var kept []string
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		if strings.ContainsAny(root, "\r\n") || !feedURL(root) {
			return "", "invalid_rsshub_base_url"
		}
		kept = append(kept, root)
	}
	instances := fetch.Instances(strings.Join(kept, "\n"))
	if len(instances) > fetch.MaxInstances {
		return "", "too_many_rsshub_base_urls"
	}
	return strings.Join(instances, "\n"), ""
}
