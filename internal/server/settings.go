package server

import (
	"context"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/yangflow/oriel/internal/ai"
	"github.com/yangflow/oriel/internal/autobackup"
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
	RSSHubBaseURLs []string `json:"rsshubBaseUrls"`
	RSSHubBaseURL  string   `json:"rsshubBaseUrl"`
	RetentionDays  int      `json:"retentionDays"`
	// The public address of this server that WebSub hubs call back, empty while WebSub is off.
	WebSubBaseURL string `json:"websubBaseUrl"`
	AIBaseURL     string `json:"aiBaseUrl"`
	AIModel       string `json:"aiModel"`
	AIAPIKeySet   bool   `json:"aiApiKeySet"`
	AILanguage    string `json:"aiLanguage"`
	// The characters sent for a summary and per piece of a translation, and their defaults.
	AISummaryLimit        int    `json:"aiSummaryLimit"`
	AIChunkLimit          int    `json:"aiChunkLimit"`
	DefaultAISummaryLimit int    `json:"defaultAiSummaryLimit"`
	DefaultAIChunkLimit   int    `json:"defaultAiChunkLimit"`
	PromptSummary         string `json:"promptSummary"`
	PromptTranslate       string `json:"promptTranslate"`
	// The built-in prompts, so a client can tell a changed prompt and restore it.
	DefaultPromptSummary   string `json:"defaultPromptSummary"`
	DefaultPromptTranslate string `json:"defaultPromptTranslate"`
	// Automatic backup: what was chosen, and how the last tries went (Unix milliseconds, 0 for never).
	AutoBackupEnabled   bool   `json:"autoBackupEnabled"`
	AutoBackupFolder    string `json:"autoBackupFolder"`
	AutoBackupFrequency string `json:"autoBackupFrequency"`
	AutoBackupKeep      int    `json:"autoBackupKeep"`
	AutoBackupLastAt    int64  `json:"autoBackupLastAt"`
	AutoBackupLastFile  string `json:"autoBackupLastFile"`
	AutoBackupFailedAt  int64  `json:"autoBackupFailedAt"`
	AutoBackupError     string `json:"autoBackupError"`
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
	websub, err := fetch.WebSubBase(r.Context(), s.store)
	if err != nil {
		internal(w, err)
		return
	}
	cfg, err := ai.Load(r.Context(), s.store)
	if err != nil {
		internal(w, err)
		return
	}
	backup, status, err := autobackup.Load(r.Context(), s.store)
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
		RSSHubBaseURLs: instances, RSSHubBaseURL: primary, RetentionDays: days, WebSubBaseURL: websub, AIBaseURL: cfg.BaseURL, AIModel: cfg.Model,
		AIAPIKeySet: cfg.APIKey != "", AILanguage: cfg.Language,
		AISummaryLimit: cfg.SummaryLimit, AIChunkLimit: cfg.ChunkLimit, DefaultAISummaryLimit: ai.DefaultSummary,
		DefaultAIChunkLimit: ai.DefaultChunk, PromptSummary: cfg.PromptSummary,
		PromptTranslate: cfg.PromptTranslate, DefaultPromptSummary: ai.DefaultPromptSummary,
		DefaultPromptTranslate: ai.DefaultPromptTranslate,
		AutoBackupEnabled:      backup.Enabled, AutoBackupFolder: backup.Folder, AutoBackupFrequency: backup.Frequency,
		AutoBackupKeep: backup.Keep, AutoBackupLastAt: status.LastAt, AutoBackupLastFile: status.LastFile,
		AutoBackupFailedAt: status.FailedAt, AutoBackupError: status.Error,
	})
}

func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RSSHubBaseURLs  *[]string `json:"rsshubBaseUrls"`
		RSSHubBaseURL   *string   `json:"rsshubBaseUrl"`
		RetentionDays   *int      `json:"retentionDays"`
		WebSubBaseURL   *string   `json:"websubBaseUrl"`
		AIBaseURL       *string   `json:"aiBaseUrl"`
		AIModel         *string   `json:"aiModel"`
		AIAPIKey        *string   `json:"aiApiKey"`
		AILanguage      *string   `json:"aiLanguage"`
		AISummaryLimit  *int      `json:"aiSummaryLimit"`
		AIChunkLimit    *int      `json:"aiChunkLimit"`
		PromptSummary   *string   `json:"promptSummary"`
		PromptTranslate *string   `json:"promptTranslate"`

		AutoBackupEnabled   *bool   `json:"autoBackupEnabled"`
		AutoBackupFolder    *string `json:"autoBackupFolder"`
		AutoBackupFrequency *string `json:"autoBackupFrequency"`
		AutoBackupKeep      *int    `json:"autoBackupKeep"`
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
	if body.WebSubBaseURL != nil {
		root := strings.TrimRight(strings.TrimSpace(*body.WebSubBaseURL), "/")
		if root != "" && !websubBase(root) {
			fail(w, http.StatusBadRequest, "invalid_websub_base_url")
			return
		}
		changes = append(changes, [2]string{fetch.WebSubSetting, root})
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
	for _, field := range []struct {
		value *int
		key   string
		high  int
	}{
		{body.AISummaryLimit, ai.SummaryLimitSetting, ai.MaxSummary},
		{body.AIChunkLimit, ai.ChunkLimitSetting, ai.MaxChunk},
	} {
		if field.value == nil {
			continue
		}
		n := *field.value
		if n != 0 && (n < ai.MinLimit || n > field.high) {
			fail(w, http.StatusBadRequest, "invalid_ai_limit")
			return
		}
		stored := ""
		if n != 0 {
			stored = strconv.Itoa(n)
		}
		changes = append(changes, [2]string{field.key, stored})
	}
	backupChanges, code, err := s.autoBackupChanges(r.Context(), body.AutoBackupEnabled, body.AutoBackupFolder, body.AutoBackupFrequency, body.AutoBackupKeep)
	if err != nil {
		internal(w, err)
		return
	}
	if code != "" {
		fail(w, http.StatusBadRequest, code)
		return
	}
	changes = append(changes, backupChanges...)
	for _, change := range changes {
		if err := s.store.SetSetting(r.Context(), change[0], change[1]); err != nil {
			internal(w, err)
			return
		}
	}
	if len(backupChanges) > 0 && s.auto != nil {
		s.auto.Poke()
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

// websubBase reports whether root can be the public address hubs call back: an https address with a host, and no
// credentials, query or fragment.
func websubBase(root string) bool {
	u, err := url.Parse(root)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.RawQuery == "" &&
		u.Fragment == "" && !strings.ContainsAny(root, "\r\n ")
}

// autoBackupChanges validates the automatic backup fields of a settings update against what is stored, and returns the
// settings to write or the error code. The folder is checked on the spot when it changes or the switch is turned on,
// not when the switch is only turned off.
func (s *Server) autoBackupChanges(ctx context.Context, enabled *bool, folder, frequency *string, keep *int) ([][2]string, string, error) {
	if enabled == nil && folder == nil && frequency == nil && keep == nil {
		return nil, "", nil
	}
	current, _, err := autobackup.Load(ctx, s.store)
	if err != nil {
		return nil, "", err
	}
	var changes [][2]string
	if frequency != nil {
		if *frequency != autobackup.Daily && *frequency != autobackup.Weekly {
			return nil, "invalid_backup_frequency", nil
		}
		changes = append(changes, [2]string{autobackup.FrequencySetting, *frequency})
	}
	if keep != nil {
		if *keep < autobackup.MinKeep || *keep > autobackup.MaxKeep {
			return nil, "invalid_backup_keep", nil
		}
		changes = append(changes, [2]string{autobackup.KeepSetting, strconv.Itoa(*keep)})
	}
	path := current.Folder
	if folder != nil {
		path = strings.TrimSpace(*folder)
		if path != "" && !filepath.IsAbs(path) {
			return nil, "invalid_backup_folder", nil
		}
		changes = append(changes, [2]string{autobackup.FolderSetting, path})
	}
	on := current.Enabled
	if enabled != nil {
		on = *enabled
		value := ""
		if on {
			value = "1"
		}
		changes = append(changes, [2]string{autobackup.EnabledSetting, value})
	}
	turnedOn := enabled != nil && *enabled
	if on && path == "" && (turnedOn || folder != nil) {
		return nil, "invalid_backup_folder", nil
	}
	if path != "" && (turnedOn || folder != nil) {
		if code := autobackup.ValidateFolder(path); code != "" {
			return nil, "backup_" + code, nil
		}
	}
	return changes, "", nil
}
