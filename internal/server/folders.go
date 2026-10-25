package server

import (
	"errors"
	"net/http"

	"github.com/yangflow/oriel/internal/store"
)

type folderJSON struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	SourceCount int    `json:"sourceCount"`
}

func folderToJSON(f store.Folder) folderJSON {
	return folderJSON{ID: f.ID, Name: f.Name, SourceCount: f.SourceCount}
}

func (s *Server) listFolders(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.Folders(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	out := make([]folderJSON, len(list))
	for i, f := range list {
		out[i] = folderToJSON(f)
	}
	reply(w, http.StatusOK, map[string]any{"folders": out})
}

func (s *Server) createFolder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if !readBody(w, r, &body) {
		return
	}
	folder, err := s.store.CreateFolder(r.Context(), body.Name, s.now())
	if replyFolderError(w, err) {
		return
	}
	reply(w, http.StatusCreated, folderToJSON(folder))
}

func (s *Server) renameFolder(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if !readBody(w, r, &body) {
		return
	}
	folder, err := s.store.RenameFolder(r.Context(), id, body.Name)
	if replyFolderError(w, err) {
		return
	}
	reply(w, http.StatusOK, folderToJSON(folder))
}

func (s *Server) deleteFolder(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if replyFolderError(w, s.store.DeleteFolder(r.Context(), id)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// replyFolderError answers a failed folder write and reports whether there was an error.
func replyFolderError(w http.ResponseWriter, err error) bool {
	var exists *store.FolderExistsError
	switch {
	case err == nil:
		return false
	case errors.Is(err, store.ErrInvalidFolderName):
		fail(w, http.StatusBadRequest, "invalid_name")
	case errors.As(err, &exists):
		reply(w, http.StatusConflict, map[string]any{"error": "folder_exists", "folderId": exists.ID})
	case errors.Is(err, store.ErrExists):
		fail(w, http.StatusConflict, "folder_exists")
	case errors.Is(err, store.ErrNotFound):
		fail(w, http.StatusNotFound, "not_found")
	default:
		internal(w, err)
	}
	return true
}

// checkFolder reports whether id is the id of a folder. When it is not it has answered the request: invalid_folder,
// or a 500 when the store failed.
func (s *Server) checkFolder(w http.ResponseWriter, r *http.Request, id int64) bool {
	if id > 0 {
		_, err := s.store.Folder(r.Context(), id)
		if err == nil {
			return true
		}
		if !errors.Is(err, store.ErrNotFound) {
			internal(w, err)
			return false
		}
	}
	fail(w, http.StatusBadRequest, "invalid_folder")
	return false
}

// folderRef is the folder of a source in JSON: null for none.
func folderRef(id int64) *int64 {
	if id == 0 {
		return nil
	}
	return &id
}
