package server

import (
	"net/http"

	"github.com/yangflow/oriel/internal/diag"
)

// diagnostics answers with the diagnostics file as plain text (docs/product.md, "诊断文件").
func (s *Server) diagnostics(w http.ResponseWriter, r *http.Request) {
	body, err := diag.Build(r.Context(), s.store, diag.Info{Version: s.version, Mode: s.mode, Log: s.log, Now: s.now()})
	if err != nil {
		internal(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(body)
}
