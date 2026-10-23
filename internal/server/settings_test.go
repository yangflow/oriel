package server

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/yangflow/oriel/internal/ai"
	"github.com/yangflow/oriel/internal/fetch"
)

func TestSettings(t *testing.T) {
	s, _ := newServer(t, "secret")
	token := login(t, s, "laptop")
	ctx := context.Background()
	for _, method := range []string{"GET", "PATCH"} {
		if w := do(s, method, "/api/settings", `{}`, nil); w.Code != http.StatusUnauthorized {
			t.Fatalf("%s without session: %d", method, w.Code)
		}
	}
	// A secret in the settings table never leaves the server.
	s.store.SetSetting(ctx, "ai_api_key", "stored-secret-value")
	w := do(s, "GET", "/api/settings", "", bearer(token))
	if body := decode(t, w); w.Code != http.StatusOK || strings.Contains(w.Body.String(), "stored-secret-value") ||
		body["rsshubBaseUrl"] != "" || len(body["rsshubBaseUrls"].([]any)) != 0 || body["retentionDays"] != 0.0 || body["aiBaseUrl"] != "" || body["aiModel"] != "" ||
		body["aiApiKeySet"] != true || body["aiLanguage"] != "zh-Hans" || body["promptSummary"] != ai.DefaultPromptSummary ||
		body["promptTranslate"] != ai.DefaultPromptTranslate || body["defaultPromptSummary"] != ai.DefaultPromptSummary ||
		body["defaultPromptTranslate"] != ai.DefaultPromptTranslate || len(body) != 34 || body["proxyUrl"] != "" || body["proxyAuthSet"] != false || body["groupSameLink"] != true || body["websubBaseUrl"] != "" || body["aiSummaryLimit"] != 24000.0 ||
		body["aiChunkLimit"] != 6000.0 || body["defaultAiSummaryLimit"] != 24000.0 || body["defaultAiChunkLimit"] != 6000.0 {
		t.Fatalf("defaults: %d %s", w.Code, w.Body)
	}

	w = do(s, "PATCH", "/api/settings", `{"rsshubBaseUrl":" http://localhost:1200/ ","retentionDays":30}`, bearer(token))
	if body := decode(t, w); w.Code != http.StatusOK || body["rsshubBaseUrl"] != "http://localhost:1200" || body["retentionDays"] != 30.0 {
		t.Fatalf("patch: %d %v", w.Code, body)
	}
	// The fetch loop reads these keys on every run, so new values apply from the next fetch and prune.
	if root, _ := s.store.Setting(ctx, fetch.RSSHubSetting); root != "http://localhost:1200" {
		t.Fatalf("stored root = %q", root)
	}
	if days, _ := s.store.Setting(ctx, fetch.RetentionSetting); days != "30" {
		t.Fatalf("stored retention = %q", days)
	}
	if body := decode(t, do(s, "PATCH", "/api/settings", `{"retentionDays":0}`, bearer(token))); body["rsshubBaseUrl"] != "http://localhost:1200" || body["retentionDays"] != 0.0 {
		t.Fatalf("only the given field changes: %v", body)
	}
	if body := decode(t, do(s, "PATCH", "/api/settings", `{"rsshubBaseUrl":""}`, bearer(token))); body["rsshubBaseUrl"] != "" {
		t.Fatalf("clear root: %v", body)
	}

	// Several instances: kept in order, blanks and repeats dropped, the first also read as the single address.
	w = do(s, "PATCH", "/api/settings", `{"rsshubBaseUrls":[" https://a.example/ ","","http://b.example","HTTPS://A.EXAMPLE"]}`, bearer(token))
	body := decode(t, w)
	if urls, _ := body["rsshubBaseUrls"].([]any); w.Code != http.StatusOK || len(urls) != 2 || urls[0] != "https://a.example" ||
		urls[1] != "http://b.example" || body["rsshubBaseUrl"] != "https://a.example" {
		t.Fatalf("list: %d %v", w.Code, body)
	}
	if raw, _ := s.store.Setting(ctx, fetch.RSSHubSetting); raw != "https://a.example\nhttp://b.example" {
		t.Fatalf("stored list = %q", raw)
	}
	// The list wins over the single address in one request; the single address alone replaces the whole list.
	if body := decode(t, do(s, "PATCH", "/api/settings", `{"rsshubBaseUrl":"http://old.example","rsshubBaseUrls":["http://c.example"]}`, bearer(token))); body["rsshubBaseUrl"] != "http://c.example" {
		t.Fatalf("list and single: %v", body)
	}
	if body := decode(t, do(s, "PATCH", "/api/settings", `{"rsshubBaseUrl":"http://old.example"}`, bearer(token))); len(body["rsshubBaseUrls"].([]any)) != 1 {
		t.Fatalf("single replaces the list: %v", body)
	}
	if body := decode(t, do(s, "PATCH", "/api/settings", `{"rsshubBaseUrls":[]}`, bearer(token))); len(body["rsshubBaseUrls"].([]any)) != 0 || body["rsshubBaseUrl"] != "" {
		t.Fatalf("clear list: %v", body)
	}
	// A value saved before the list existed, with its trailing slash, reads as a list of one.
	if err := s.store.SetSetting(ctx, fetch.RSSHubSetting, "http://legacy.example/"); err != nil {
		t.Fatal(err)
	}
	if body := decode(t, do(s, "GET", "/api/settings", "", bearer(token))); body["rsshubBaseUrl"] != "http://legacy.example" ||
		len(body["rsshubBaseUrls"].([]any)) != 1 {
		t.Fatalf("legacy value: %v", body)
	}

	for body, code := range map[string]string{
		`{"rsshubBaseUrl":"ftp://example.org"}`:                     "invalid_rsshub_base_url",
		`{"rsshubBaseUrl":"localhost:1200"}`:                        "invalid_rsshub_base_url",
		`{"rsshubBaseUrls":["http://a.example","localhost:1200"]}`:  "invalid_rsshub_base_url",
		`{"rsshubBaseUrls":["http://a.example\nhttp://b.example"]}`: "invalid_rsshub_base_url",
		`{"rsshubBaseUrls":"http://a.example"}`:                     "invalid_body",
		`{"rsshubBaseUrls":["http://1.example","http://2.example","http://3.example","http://4.example","http://5.example","http://6.example"]}`: "too_many_rsshub_base_urls",
		`{"retentionDays":-1}`:                                    "invalid_retention_days",
		`{"retentionDays":3651}`:                                  "invalid_retention_days",
		`{"retentionDays":"30"}`:                                  "invalid_body",
		`{"retentionDays":1.5}`:                                   "invalid_body",
		`{"aiApiKeySet":true}`:                                    "invalid_body",
		`{"groupSameLink":"yes"}`:                                 "invalid_body",
		`{"aiBaseUrl":"localhost:11434"}`:                         "invalid_ai_base_url",
		`{"aiModel":"` + strings.Repeat("m", 201) + `"}`:          "invalid_ai_model",
		`{"aiApiKey":"` + strings.Repeat("k", 4097) + `"}`:        "invalid_ai_api_key",
		`{"aiLanguage":"fr"}`:                                     "invalid_ai_language",
		`{"aiSummaryLimit":499}`:                                  "invalid_ai_limit",
		`{"aiSummaryLimit":100001}`:                               "invalid_ai_limit",
		`{"aiChunkLimit":20001}`:                                  "invalid_ai_limit",
		`{"aiChunkLimit":-1}`:                                     "invalid_ai_limit",
		`{"aiAutoDailyLimit":10001}`:                              "invalid_ai_limit",
		`{"aiAutoFetchLimit":101}`:                                "invalid_ai_limit",
		`{"aiAutoFetchLimit":-1}`:                                 "invalid_ai_limit",
		`{"promptSummary":"` + strings.Repeat("p", 4001) + `"}`:   "invalid_prompt",
		`{"promptTranslate":"` + strings.Repeat("p", 4001) + `"}`: "invalid_prompt",
		`{"retentionDays":7,"rsshubBaseUrl":"file:///"}`:          "invalid_rsshub_base_url",
	} {
		w := do(s, "PATCH", "/api/settings", body, bearer(token))
		if w.Code != http.StatusBadRequest || decode(t, w)["error"] != code {
			t.Errorf("%s: %d %s", body, w.Code, w.Body)
		}
	}
	// A rejected request changes nothing, not even its valid fields.
	if days, _ := s.store.Setting(ctx, fetch.RetentionSetting); days != "0" {
		t.Fatalf("rejected patch stored retention %q", days)
	}

	// AI settings: the endpoint loses its trailing slash, the key is write-only, an empty prompt restores the default.
	w = do(s, "PATCH", "/api/settings", `{"aiBaseUrl":" http://192.168.1.5:11434/v1/ ","aiModel":" qwen ","aiApiKey":"","aiLanguage":"en","promptSummary":" Short {language} ","promptTranslate":""}`, bearer(token))
	if body := decode(t, w); w.Code != http.StatusOK || body["aiBaseUrl"] != "http://192.168.1.5:11434/v1" || body["aiModel"] != "qwen" ||
		body["aiApiKeySet"] != false || body["aiLanguage"] != "en" || body["promptSummary"] != "Short {language}" ||
		body["promptTranslate"] != ai.DefaultPromptTranslate {
		t.Fatalf("AI settings: %d %v", w.Code, body)
	}
	w = do(s, "PATCH", "/api/settings", `{"aiSummaryLimit":4000,"aiChunkLimit":1500}`, bearer(token))
	if body := decode(t, w); w.Code != http.StatusOK || body["aiSummaryLimit"] != 4000.0 || body["aiChunkLimit"] != 1500.0 {
		t.Fatalf("limits: %d %s", w.Code, w.Body)
	}
	w = do(s, "PATCH", "/api/settings", `{"aiSummaryLimit":0,"aiChunkLimit":0}`, bearer(token))
	if body := decode(t, w); w.Code != http.StatusOK || body["aiSummaryLimit"] != 24000.0 || body["aiChunkLimit"] != 6000.0 {
		t.Fatalf("limits restored: %d %s", w.Code, w.Body)
	}
	w = do(s, "PATCH", "/api/settings", `{"aiApiKey":" new-value ","promptSummary":"  "}`, bearer(token))
	if body := decode(t, w); body["aiApiKeySet"] != true || body["promptSummary"] != ai.DefaultPromptSummary || strings.Contains(w.Body.String(), "new-value") {
		t.Fatalf("key and prompt reset: %s", w.Body)
	}
	if key, _ := s.store.Setting(ctx, ai.APIKeySetting); key != "new-value" {
		t.Fatalf("stored key = %q", key)
	}

	// A stored value the fetch loop reads as forever reads back as 0.
	s.store.SetSetting(ctx, fetch.RetentionSetting, "a week")
	if body := decode(t, do(s, "GET", "/api/settings", "", bearer(token))); body["retentionDays"] != 0.0 {
		t.Fatalf("unreadable retention: %v", body)
	}
}

// The caps on automatic AI work are saved, 0 puts the default back, and settings says where the text goes.
func TestSettingsAutomaticCaps(t *testing.T) {
	s, _ := newServer(t, "secret")
	token := login(t, s, "laptop")
	w := do(s, "PATCH", "/api/settings", `{"aiAutoDailyLimit":50,"aiAutoFetchLimit":5,"aiBaseUrl":"http://localhost:11434/v1"}`, bearer(token))
	body := decode(t, w)
	if w.Code != http.StatusOK || body["aiAutoDailyLimit"] != 50.0 || body["aiAutoFetchLimit"] != 5.0 ||
		body["defaultAiAutoDailyLimit"] != 200.0 || body["defaultAiAutoFetchLimit"] != 10.0 || body["aiEndpointLocal"] != true ||
		body["aiAutoCallsToday"] != 0.0 {
		t.Fatalf("patch: %d %v", w.Code, body)
	}
	w = do(s, "PATCH", "/api/settings", `{"aiAutoDailyLimit":0,"aiBaseUrl":"https://api.example.com/v1"}`, bearer(token))
	if body := decode(t, w); w.Code != http.StatusOK || body["aiAutoDailyLimit"] != 200.0 || body["aiAutoFetchLimit"] != 5.0 ||
		body["aiEndpointLocal"] != false {
		t.Fatalf("reset: %d %v", w.Code, body)
	}
}

// A rejected WebSub callback address is not stored, trailing slashes are cut, another field leaves the address alone,
// and an empty value turns WebSub off. The plain bad forms are in TestWebSubSettingsAndSource.
func TestSettingsWebSubBaseURL(t *testing.T) {
	s, _ := newServer(t, "secret")
	token := login(t, s, "laptop")
	for _, root := range []string{"https://oriel .example.com", "https://oriel.example.com?x=1"} {
		w := do(s, "PATCH", "/api/settings", `{"websubBaseUrl":"`+root+`"}`, bearer(token))
		if w.Code != http.StatusBadRequest || decode(t, w)["error"] != "invalid_websub_base_url" {
			t.Errorf("%q: %d %s", root, w.Code, w.Body)
		}
	}
	if root, _ := s.store.Setting(context.Background(), fetch.WebSubSetting); root != "" {
		t.Fatalf("a rejected address was stored: %q", root)
	}
	w := do(s, "PATCH", "/api/settings", `{"websubBaseUrl":"  https://oriel.example.com/hooks// "}`, bearer(token))
	if body := decode(t, w); w.Code != http.StatusOK || body["websubBaseUrl"] != "https://oriel.example.com/hooks" {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	if body := decode(t, do(s, "PATCH", "/api/settings", `{"retentionDays":7}`, bearer(token))); body["websubBaseUrl"] != "https://oriel.example.com/hooks" {
		t.Fatalf("another field changed it: %v", body)
	}
	if body := decode(t, do(s, "PATCH", "/api/settings", `{"websubBaseUrl":"  "}`, bearer(token))); body["websubBaseUrl"] != "" {
		t.Fatalf("clear: %v", body)
	}
}

// An empty patch and a patch of unknown or mistyped fields change nothing, and a bad field rejects the whole request
// even when the AI fields before it are valid.
func TestSettingsPatchIsAllOrNothing(t *testing.T) {
	s, _ := newServer(t, "secret")
	token := login(t, s, "laptop")
	ctx := context.Background()
	before := do(s, "GET", "/api/settings", "", bearer(token)).Body.String()
	if w := do(s, "PATCH", "/api/settings", `{}`, bearer(token)); w.Code != http.StatusOK || w.Body.String() != before {
		t.Fatalf("empty patch: %d %s", w.Code, w.Body)
	}
	for _, body := range []string{`{"unknown":1}`, `[]`, `{"aiModel":"m"} {"aiModel":"n"}`, `{"aiModel":5}`} {
		if w := do(s, "PATCH", "/api/settings", body, bearer(token)); w.Code != http.StatusBadRequest || decode(t, w)["error"] != "invalid_body" {
			t.Errorf("%s: %d %s", body, w.Code, w.Body)
		}
	}
	w := do(s, "PATCH", "/api/settings", `{"aiModel":"qwen","aiBaseUrl":"https://api.example.com/v1","aiSummaryLimit":100}`, bearer(token))
	if w.Code != http.StatusBadRequest || decode(t, w)["error"] != "invalid_ai_limit" {
		t.Fatalf("mixed patch: %d %s", w.Code, w.Body)
	}
	for _, key := range []string{ai.ModelSetting, ai.BaseURLSetting} {
		if value, _ := s.store.Setting(ctx, key); value != "" {
			t.Errorf("%s = %q after a rejected patch", key, value)
		}
	}
	if after := do(s, "GET", "/api/settings", "", bearer(token)).Body.String(); after != before {
		t.Fatalf("settings changed:\n%s\n%s", before, after)
	}
	// The edges of a limit are accepted.
	for body, want := range map[string]float64{`{"aiSummaryLimit":500}`: 500, `{"aiSummaryLimit":100000}`: 100000} {
		if got := decode(t, do(s, "PATCH", "/api/settings", body, bearer(token)))["aiSummaryLimit"]; got != want {
			t.Errorf("%s: %v", body, got)
		}
	}
}

func TestSettingsProxy(t *testing.T) {
	s, _ := newServer(t, "secret")
	token := login(t, s, "laptop")
	ctx := context.Background()
	for _, body := range []string{`{"proxyUrl":"ftp://127.0.0.1:21"}`, `{"proxyUrl":"not a url"}`, `{"proxyUrl":"http://"}`} {
		if w := do(s, "PATCH", "/api/settings", body, bearer(token)); w.Code != http.StatusBadRequest || decode(t, w)["error"] != "invalid_proxy_url" {
			t.Errorf("patch %s: %d %s", body, w.Code, w.Body)
		}
	}
	if w := do(s, "PATCH", "/api/settings", `{"proxyUser":"`+strings.Repeat("a", fetch.MaxProxyField+1)+`"}`, bearer(token)); w.Code != http.StatusBadRequest || decode(t, w)["error"] != "invalid_proxy_credentials" {
		t.Errorf("long user: %d %s", w.Code, w.Body)
	}

	w := do(s, "PATCH", "/api/settings", `{"proxyUrl":" socks5://127.0.0.1:1080 ","proxyUser":"alice","proxyPassword":"pw-secret-1"}`, bearer(token))
	body := decode(t, w)
	if w.Code != http.StatusOK || body["proxyUrl"] != "socks5://127.0.0.1:1080" || body["proxyAuthSet"] != true ||
		strings.Contains(w.Body.String(), "alice") || strings.Contains(w.Body.String(), "pw-secret-1") {
		t.Fatalf("set: %d %s", w.Code, w.Body)
	}
	if w := do(s, "GET", "/api/settings", "", bearer(token)); strings.Contains(w.Body.String(), "alice") || strings.Contains(w.Body.String(), "pw-secret-1") || decode(t, w)["proxyAuthSet"] != true {
		t.Fatalf("get: %s", w.Body)
	}
	if u, _ := s.store.Setting(ctx, fetch.ProxyUserSetting); u != "alice" {
		t.Fatalf("stored user %q", u)
	}

	// The same address keeps the credentials; another address deletes them.
	if body := decode(t, do(s, "PATCH", "/api/settings", `{"proxyUrl":"socks5://127.0.0.1:1080"}`, bearer(token))); body["proxyAuthSet"] != true {
		t.Fatalf("same address: %v", body)
	}
	if body := decode(t, do(s, "PATCH", "/api/settings", `{"proxyUrl":"http://127.0.0.1:7890"}`, bearer(token))); body["proxyUrl"] != "http://127.0.0.1:7890" || body["proxyAuthSet"] != false {
		t.Fatalf("new address: %v", body)
	}
	if p, _ := s.store.Setting(ctx, fetch.ProxyPasswordSetting); p != "" {
		t.Fatalf("password kept: %q", p)
	}
	if body := decode(t, do(s, "PATCH", "/api/settings", `{"proxyUrl":"http://127.0.0.1:1","proxyPassword":"pw2"}`, bearer(token))); body["proxyAuthSet"] != true {
		t.Fatalf("new address with password: %v", body)
	}
	if body := decode(t, do(s, "PATCH", "/api/settings", `{"proxyUrl":""}`, bearer(token))); body["proxyUrl"] != "" || body["proxyAuthSet"] != false {
		t.Fatalf("cleared: %v", body)
	}
}
