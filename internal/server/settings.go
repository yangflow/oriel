package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/yangflow/oriel/internal/fetch"
)

// settingsJSON lists every setting a client may read. Secrets are never listed, only whether they are set.
type settingsJSON struct {
	RSSHubBaseURL string `json:"rsshubBaseUrl"`
	RetentionDays int    `json:"retentionDays"`
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
	reply(w, http.StatusOK, settingsJSON{RSSHubBaseURL: root, RetentionDays: days})
}

func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RSSHubBaseURL *string `json:"rsshubBaseUrl"`
		RetentionDays *int    `json:"retentionDays"`
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
	for _, change := range changes {
		if err := s.store.SetSetting(r.Context(), change[0], change[1]); err != nil {
			internal(w, err)
			return
		}
	}
	s.getSettings(w, r)
}
