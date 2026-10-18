// Package ai summarizes and translates entries through the user's OpenAI-compatible endpoint.
// See docs/product.md "AI 摘要和翻译".
package ai

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/yangflow/oriel/internal/store"
)

// Setting keys.
const (
	BaseURLSetting         = "ai_base_url"
	ModelSetting           = "ai_model"
	APIKeySetting          = "ai_api_key"
	LanguageSetting        = "ai_language"
	PromptSummarySetting   = "prompt_summary"
	PromptTranslateSetting = "prompt_translate"
)

// Built-in prompts, used while the global prompt is not set.
const (
	DefaultPromptSummary   = "Summarize the article in {language} in three to five sentences. Reply with the summary only."
	DefaultPromptTranslate = "Translate the text into {language}. Keep the paragraphs and their order, and the title on the first line. Reply with the translation only."
)

const (
	summaryLimit     = 24000
	translationLimit = 60000
	chunkLimit       = 6000
	maxResponse      = 1 << 20
)

// Languages maps the target languages to the names used in prompts.
var Languages = map[string]string{"zh-Hans": "简体中文", "en": "English"}

// Code is a stable error code of a failed AI call.
type Code string

func (c Code) Error() string { return string(c) }

// Error codes.
const (
	ErrNotConfigured Code = "ai_not_configured"
	ErrUnreachable   Code = "ai_unreachable"
	ErrTimeout       Code = "ai_timeout"
	ErrUnauthorized  Code = "ai_unauthorized"
	ErrRateLimited   Code = "ai_rate_limited"
	ErrUpstream      Code = "ai_upstream"
	ErrEmpty         Code = "ai_empty"
)

// Config is the AI part of the settings.
type Config struct {
	BaseURL         string
	Model           string
	APIKey          string
	Language        string
	PromptSummary   string
	PromptTranslate string
}

// Configured reports whether both the endpoint and the model are set.
func (c Config) Configured() bool { return c.BaseURL != "" && c.Model != "" }

// Load reads the AI settings, filling in the default language and prompts.
func Load(ctx context.Context, st *store.Store) (Config, error) {
	var c Config
	for _, field := range []struct {
		key string
		dst *string
	}{
		{BaseURLSetting, &c.BaseURL}, {ModelSetting, &c.Model}, {APIKeySetting, &c.APIKey},
		{LanguageSetting, &c.Language}, {PromptSummarySetting, &c.PromptSummary},
		{PromptTranslateSetting, &c.PromptTranslate},
	} {
		value, err := st.Setting(ctx, field.key)
		if err != nil {
			return Config{}, err
		}
		*field.dst = value
	}
	if _, ok := Languages[c.Language]; !ok {
		c.Language = "zh-Hans"
	}
	if c.PromptSummary == "" {
		c.PromptSummary = DefaultPromptSummary
	}
	if c.PromptTranslate == "" {
		c.PromptTranslate = DefaultPromptTranslate
	}
	return c, nil
}

// Configured reports whether the stored settings name an endpoint and a model.
func Configured(ctx context.Context, st *store.Store) (bool, error) {
	c, err := Load(ctx, st)
	return c.Configured(), err
}

// job is one task prepared for the endpoint.
type job struct {
	prompt      string
	chunks      []string
	fingerprint string
}

// prepare builds the prompt and the text to send. It reports false while an article still waits for its full text.
func prepare(c Config, in store.AIInput, task string) (job, bool) {
	if in.Kind == "article" && in.ExtractStatus == "pending" {
		return job{}, false
	}
	prompt, override, limit := c.PromptSummary, in.PromptSummary, summaryLimit
	if task == store.TaskTranslation {
		prompt, override, limit = c.PromptTranslate, in.PromptTranslate, translationLimit
	}
	if strings.TrimSpace(override) != "" {
		prompt = override
	}
	prompt = strings.ReplaceAll(prompt, "{language}", Languages[c.Language])
	var paragraphs []string
	if title := strings.Join(strings.Fields(in.Title), " "); title != "" {
		paragraphs = append(paragraphs, title)
	}
	for _, body := range []string{in.ExtractedHTML, in.ContentHTML, in.Summary} {
		if p := Paragraphs(body); len(p) > 0 {
			paragraphs = append(paragraphs, p...)
			break
		}
	}
	text := truncate(strings.Join(paragraphs, "\n\n"), limit)
	sum := sha256.Sum256([]byte(strings.Join([]string{c.Model, prompt, c.Language, text}, "\x00")))
	j := job{prompt: prompt, fingerprint: hex.EncodeToString(sum[:])}
	if text == "" {
		return j, true
	}
	if task == store.TaskTranslation {
		j.chunks = chunks(strings.Split(text, "\n\n"), chunkLimit)
	} else {
		j.chunks = []string{text}
	}
	return j, true
}

// truncate keeps at most limit characters.
func truncate(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:limit]))
}

// chunks joins paragraphs into pieces of at most limit characters, splitting a longer paragraph.
func chunks(paragraphs []string, limit int) []string {
	var out []string
	var current []string
	size := 0
	flush := func() {
		if len(current) > 0 {
			out = append(out, strings.Join(current, "\n\n"))
			current, size = nil, 0
		}
	}
	for _, p := range paragraphs {
		runes := []rune(p)
		for len(runes) > limit {
			flush()
			out = append(out, string(runes[:limit]))
			runes = runes[limit:]
		}
		n := len(runes)
		if size > 0 && size+2+n > limit {
			flush()
		}
		if size > 0 {
			size += 2
		}
		current = append(current, string(runes))
		size += n
	}
	flush()
	return out
}

// blocks are the elements that end a paragraph.
var blocks = map[atom.Atom]bool{
	atom.P: true, atom.Div: true, atom.Br: true, atom.Li: true, atom.Ul: true, atom.Ol: true, atom.Dl: true,
	atom.Dt: true, atom.Dd: true, atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true, atom.H5: true,
	atom.H6: true, atom.Blockquote: true, atom.Pre: true, atom.Table: true, atom.Tr: true, atom.Hr: true,
	atom.Section: true, atom.Article: true, atom.Header: true, atom.Footer: true, atom.Figure: true,
	atom.Figcaption: true, atom.Aside: true,
}

// Paragraphs is the visible text of an HTML fragment or plain text, one paragraph per element.
func Paragraphs(fragment string) []string {
	var out []string
	var b strings.Builder
	flush := func() {
		if p := strings.Join(strings.Fields(b.String()), " "); p != "" {
			out = append(out, p)
		}
		b.Reset()
	}
	if !strings.ContainsAny(fragment, "<&") {
		for _, p := range strings.Split(strings.ReplaceAll(fragment, "\r\n", "\n"), "\n\n") {
			b.WriteString(p)
			flush()
		}
		return out
	}
	nodes, err := html.ParseFragment(strings.NewReader(fragment), &html.Node{Type: html.ElementNode, DataAtom: atom.Div, Data: "div"})
	if err != nil {
		return nil
	}
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		switch {
		case n.Type == html.TextNode:
			b.WriteString(n.Data)
			return
		case n.Type == html.ElementNode && (n.DataAtom == atom.Script || n.DataAtom == atom.Style):
			return
		}
		block := n.Type == html.ElementNode && blocks[n.DataAtom]
		if block {
			flush()
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
		if block {
			flush()
		}
	}
	for _, n := range nodes {
		visit(n)
	}
	flush()
	return out
}

// complete sends one chat request and returns the reply and the model that answered.
// It returns ctx's error when ctx ends, and a Code for everything else.
func complete(ctx context.Context, client *http.Client, c Config, userAgent, system, user string) (string, string, error) {
	body, err := json.Marshal(map[string]any{
		"model":  c.Model,
		"stream": false,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
	})
	if err != nil {
		return "", "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", "", ErrUnreachable
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", transportError(ctx, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return "", "", ErrUnauthorized
	case resp.StatusCode == http.StatusTooManyRequests:
		return "", "", ErrRateLimited
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return "", "", ErrUpstream
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return "", "", transportError(ctx, err)
	}
	if len(raw) > maxResponse {
		return "", "", ErrUpstream
	}
	var reply struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(raw, &reply) != nil || len(reply.Choices) == 0 {
		return "", "", ErrUpstream
	}
	text := strings.TrimSpace(reply.Choices[0].Message.Content)
	if text == "" {
		return "", "", ErrUpstream
	}
	model := reply.Model
	if model == "" {
		model = c.Model
	}
	return text, model, nil
}

// transportError tells a timeout from an endpoint that cannot be reached.
func transportError(ctx context.Context, err error) error {
	if context.Cause(ctx) == errCallTimeout {
		return ErrTimeout
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return ErrTimeout
	}
	return ErrUnreachable
}

// errCallTimeout is the cause of a call's own deadline, so it can be told from the caller's.
var errCallTimeout = errors.New("AI call timed out")
