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

func TestFolderAPI(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = append(seen, r.Method+" "+r.URL.RequestURI()+" "+string(body))
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/folders":
			w.Write([]byte(`{"folders":[{"id":3,"name":"Tech","sourceCount":2}]}`))
		case r.Method == "POST":
			if strings.Contains(string(body), "Tech") {
				w.WriteHeader(http.StatusConflict)
				w.Write([]byte(`{"error":"folder_exists","folderId":3}`))
				return
			}
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"id":4,"name":"News","sourceCount":0}`))
		case r.Method == "PATCH":
			w.Write([]byte(`{"id":4,"name":"Daily","sourceCount":0}`))
		case r.Method == "DELETE":
			w.WriteHeader(http.StatusNoContent)
		default:
			w.Write([]byte(`{"entries":[],"nextCursor":null}`))
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	session := Session{Server: srv.URL, Token: "t1"}

	if list, err := session.Folders(ctx); err != nil || len(list) != 1 || list[0] != (Folder{ID: 3, Name: "Tech", SourceCount: 2}) {
		t.Errorf("Folders = %+v, %v", list, err)
	}
	if f, err := session.CreateFolder(ctx, "News"); err != nil || f.ID != 4 || f.Name != "News" {
		t.Errorf("CreateFolder = %+v, %v", f, err)
	}
	var apiErr *APIError
	if _, err := session.CreateFolder(ctx, "Tech"); !errors.As(err, &apiErr) || apiErr.Code != "folder_exists" {
		t.Errorf("CreateFolder twice: %v", err)
	}
	if f, err := session.RenameFolder(ctx, 4, "Daily"); err != nil || f.Name != "Daily" {
		t.Errorf("RenameFolder = %+v, %v", f, err)
	}
	if err := session.DeleteFolder(ctx, 4); err != nil {
		t.Errorf("DeleteFolder: %v", err)
	}
	none, in := int64(0), int64(4)
	if _, err := session.UpdateSource(ctx, 9, SourcePatch{FolderID: &in}); err != nil {
		t.Errorf("UpdateSource in: %v", err)
	}
	if _, err := session.UpdateSource(ctx, 9, SourcePatch{FolderID: &none}); err != nil {
		t.Errorf("UpdateSource out: %v", err)
	}
	if _, err := session.Entries(ctx, Query{Folder: 4, Kind: "video"}); err != nil {
		t.Errorf("Entries: %v", err)
	}
	if _, err := session.MarkRead(ctx, ReadScope{Folder: 4, Kind: "video"}); err != nil {
		t.Errorf("MarkRead: %v", err)
	}

	want := []string{
		"GET /api/folders ",
		`POST /api/folders {"name":"News"}`,
		`POST /api/folders {"name":"Tech"}`,
		`PATCH /api/folders/4 {"name":"Daily"}`,
		"DELETE /api/folders/4 ",
		`PATCH /api/sources/9 {"folderId":4}`,
		`PATCH /api/sources/9 {"folderId":0}`,
		"GET /api/entries?folder=4&kind=video ",
		`POST /api/entries/read {"kind":"video","folder":4}`,
	}
	if strings.Join(seen, "\n") != strings.Join(want, "\n") {
		t.Errorf("requests:\n%s\nwant:\n%s", strings.Join(seen, "\n"), strings.Join(want, "\n"))
	}
}
