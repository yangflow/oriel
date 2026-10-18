package server

import (
	"errors"
	"net/http"

	"github.com/yangflow/oriel/internal/ai"
	"github.com/yangflow/oriel/internal/store"
)

type aiResultJSON struct {
	Status    string `json:"status"`
	Text      string `json:"text"`
	Error     string `json:"error"`
	Model     string `json:"model"`
	UpdatedAt *int64 `json:"updatedAt"`
}

func aiResultToJSON(r store.AIResult) *aiResultJSON {
	return &aiResultJSON{Status: r.Status, Text: r.Text, Error: r.Error, Model: r.Model, UpdatedAt: millis(r.UpdatedAt)}
}

func (s *Server) getAI(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	results, err := s.store.AIResults(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	body := map[string]*aiResultJSON{"summary": nil, "translation": nil}
	for task, result := range results {
		body[task] = aiResultToJSON(result)
	}
	reply(w, http.StatusOK, body)
}

func (s *Server) requestSummary(w http.ResponseWriter, r *http.Request) {
	s.requestAI(w, r, store.TaskSummary)
}

func (s *Server) requestTranslation(w http.ResponseWriter, r *http.Request) {
	s.requestAI(w, r, store.TaskTranslation)
}

func (s *Server) requestAI(w http.ResponseWriter, r *http.Request, task string) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	result, err := s.ai.Request(r.Context(), id, task)
	switch {
	case errors.Is(err, ai.ErrNotConfigured):
		fail(w, http.StatusConflict, string(ai.ErrNotConfigured))
	case errors.Is(err, store.ErrNotFound):
		fail(w, http.StatusNotFound, "not_found")
	case err != nil:
		internal(w, err)
	case result.Status == "pending":
		reply(w, http.StatusAccepted, aiResultToJSON(result))
	default:
		reply(w, http.StatusOK, aiResultToJSON(result))
	}
}

func (s *Server) testAI(w http.ResponseWriter, r *http.Request) {
	model, err := s.ai.Test(r.Context())
	var code ai.Code
	switch {
	case errors.Is(err, ai.ErrNotConfigured):
		fail(w, http.StatusConflict, string(ai.ErrNotConfigured))
	case errors.As(err, &code):
		fail(w, http.StatusBadGateway, string(code))
	case err != nil:
		internal(w, err)
	default:
		reply(w, http.StatusOK, map[string]string{"model": model})
	}
}
