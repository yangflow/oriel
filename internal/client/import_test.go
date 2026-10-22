package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestImportAPI(t *testing.T) {
	ctx := context.Background()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = append(seen, r.Method+" "+r.URL.Path+" "+string(body))
		switch r.URL.Path {
		case "/api/import/start":
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte(`{"phase":"fetching","service":"miniflux","progress":{"stage":"","done":0,"total":0},"counts":{}}`))
		case "/api/import":
			w.Write([]byte(`{"phase":"ready","service":"miniflux","progress":{},"counts":{"sources":3,"folders":1,"starred":4,"read":5},"extra":1}`))
		case "/api/import/confirm":
			w.Write([]byte(`{"phase":"done","result":{"addedSources":2,"skipped":[{"kind":"source","title":"G","address":"ftp://x","error":"invalid_url"}],"skippedTotal":1}}`))
		case "/api/import/cancel":
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte(`{"error":"import_not_ready"}`))
		}
	}))
	defer srv.Close()
	session := Session{Server: srv.URL}

	status, err := session.StartImport(ctx, ImportRequest{Service: ServiceMiniflux, BaseURL: "https://m.example", Credential: "fake-key"})
	if err != nil || status.Phase != "fetching" {
		t.Errorf("StartImport = %+v, %v", status, err)
	}
	if status, err = session.ImportStatus(ctx); err != nil || status.Counts != (ImportCounts{Sources: 3, Folders: 1, Starred: 4, Read: 5}) {
		t.Errorf("ImportStatus = %+v, %v", status, err)
	}
	if status, err = session.ConfirmImport(ctx); err != nil || status.Result == nil || status.Result.AddedSources != 2 ||
		status.Result.Skipped[0].Error != "invalid_url" {
		t.Errorf("ConfirmImport = %+v, %v", status, err)
	}
	var apiErr *APIError
	if _, err := session.CancelImport(ctx); !errors.As(err, &apiErr) || apiErr.Code != "import_not_ready" {
		t.Errorf("CancelImport: %v", err)
	}
	want := []string{
		`POST /api/import/start {"service":"miniflux","baseUrl":"https://m.example","credential":"fake-key"}`,
		"GET /api/import ",
		"POST /api/import/confirm ",
		"POST /api/import/cancel ",
	}
	if strings.Join(seen, "\n") != strings.Join(want, "\n") {
		t.Errorf("requests:\n%s\nwant:\n%s", strings.Join(seen, "\n"), strings.Join(want, "\n"))
	}
}
