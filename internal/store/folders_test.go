package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFoldersCreateRenameDeleteAndKeepTheirSources(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	now := time.Now()

	tech, err := s.CreateFolder(ctx, "  Tech  ", now)
	if err != nil || tech.Name != "Tech" || tech.SourceCount != 0 {
		t.Fatalf("CreateFolder = %+v, %v", tech, err)
	}
	var exists *FolderExistsError
	if _, err := s.CreateFolder(ctx, "tech", now); !errors.Is(err, ErrExists) || !errors.As(err, &exists) || exists.ID != tech.ID {
		t.Fatalf("a duplicate name = %v, want a FolderExistsError for %d", err, tech.ID)
	}
	for _, name := range []string{"", "   ", strings.Repeat("长", MaxFolderName+1)} {
		if _, err := s.CreateFolder(ctx, name, now); !errors.Is(err, ErrInvalidFolderName) {
			t.Fatalf("CreateFolder(%q) = %v, want ErrInvalidFolderName", name, err)
		}
	}
	news, err := s.CreateFolder(ctx, "News", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RenameFolder(ctx, news.ID, "TECH"); !errors.Is(err, ErrExists) {
		t.Fatalf("renaming onto another name = %v, want ErrExists", err)
	}
	if got, err := s.RenameFolder(ctx, news.ID, "Daily"); err != nil || got.Name != "Daily" {
		t.Fatalf("RenameFolder = %+v, %v", got, err)
	}
	if _, err := s.RenameFolder(ctx, 999, "X"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("renaming a missing folder = %v", err)
	}

	src, err := s.CreateSource(ctx, NewSource{RSSHubPath: "/a", Kind: "article", FetchInterval: time.Hour, FolderID: tech.ID}, "A", now)
	if err != nil || src.FolderID != tech.ID {
		t.Fatalf("CreateSource in a folder = %+v, %v", src, err)
	}
	other, err := s.CreateSource(ctx, NewSource{RSSHubPath: "/b", Kind: "article", FetchInterval: time.Hour}, "B", now)
	if err != nil || other.FolderID != 0 {
		t.Fatalf("CreateSource = %+v, %v", other, err)
	}
	list, err := s.Folders(ctx)
	if err != nil || len(list) != 2 || list[0].Name != "Daily" || list[1].Name != "Tech" || list[1].SourceCount != 1 {
		t.Fatalf("Folders = %+v, %v", list, err)
	}

	missing := int64(999)
	if _, err := s.UpdateSource(ctx, other.ID, SourcePatch{FolderID: &missing}, now); !errors.Is(err, ErrNoFolder) {
		t.Fatalf("moving into a missing folder = %v, want ErrNoFolder", err)
	}
	if got, err := s.UpdateSource(ctx, other.ID, SourcePatch{FolderID: &tech.ID}, now); err != nil || got.FolderID != tech.ID {
		t.Fatalf("moving in = %+v, %v", got, err)
	}
	zero := int64(0)
	if got, err := s.UpdateSource(ctx, other.ID, SourcePatch{FolderID: &zero}, now); err != nil || got.FolderID != 0 {
		t.Fatalf("moving out = %+v, %v", got, err)
	}

	if err := s.DeleteFolder(ctx, tech.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Source(ctx, src.ID); err != nil || got.FolderID != 0 {
		t.Fatalf("a source after its folder was deleted = %+v, %v", got, err)
	}
	if err := s.DeleteFolder(ctx, tech.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleting twice = %v", err)
	}
}

func TestEnsureFolderReusesByNameAndFileSourceKeepsAFiledSource(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	now := time.Now()

	id, created, err := s.EnsureFolder(ctx, " Blogs ", now)
	if err != nil || !created {
		t.Fatalf("EnsureFolder = %d, %v, %v", id, created, err)
	}
	again, created, err := s.EnsureFolder(ctx, "blogs", now)
	if err != nil || created || again != id {
		t.Fatalf("EnsureFolder again = %d, %v, %v; want %d reused", again, created, err, id)
	}
	long, _, err := s.EnsureFolder(ctx, strings.Repeat("长", MaxFolderName+5), now)
	if err != nil {
		t.Fatal(err)
	}
	if f, _ := s.Folder(ctx, long); len([]rune(f.Name)) != MaxFolderName {
		t.Fatalf("a long name is cut to %d characters, got %d", MaxFolderName, len([]rune(f.Name)))
	}
	if _, _, err := s.EnsureFolder(ctx, "  ", now); !errors.Is(err, ErrInvalidFolderName) {
		t.Fatalf("EnsureFolder(blank) = %v", err)
	}

	src, err := s.CreateSource(ctx, NewSource{RSSHubPath: "/a", Kind: "article", FetchInterval: time.Hour}, "A", now)
	if err != nil {
		t.Fatal(err)
	}
	if moved, err := s.FileSource(ctx, src.ID, id); err != nil || !moved {
		t.Fatalf("FileSource = %v, %v", moved, err)
	}
	if moved, err := s.FileSource(ctx, src.ID, long); err != nil || moved {
		t.Fatalf("FileSource of a filed source = %v, %v; want it left alone", moved, err)
	}
	if got, _ := s.Source(ctx, src.ID); got.FolderID != id {
		t.Fatalf("folder = %d, want %d", got.FolderID, id)
	}
}

func TestFolderFilterListsOnlyThatFoldersEntries(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "oriel.sqlite"))
	now := time.Now()
	folder, err := s.CreateFolder(ctx, "Tech", now)
	if err != nil {
		t.Fatal(err)
	}
	in, err := s.CreateSource(ctx, NewSource{RSSHubPath: "/in", Kind: "article", FetchInterval: time.Hour, FolderID: folder.ID}, "In", now)
	if err != nil {
		t.Fatal(err)
	}
	hidden, err := s.CreateSource(ctx, NewSource{RSSHubPath: "/hid", Kind: "article", FetchInterval: time.Hour, FolderID: folder.ID, HideFromAll: true}, "Hid", now)
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.CreateSource(ctx, NewSource{RSSHubPath: "/out", Kind: "article", FetchInterval: time.Hour}, "Out", now)
	if err != nil {
		t.Fatal(err)
	}
	add := func(id int64, guid string) int64 {
		at := now.Add(-time.Hour).UnixMilli()
		res, err := s.db.ExecContext(ctx, `INSERT INTO entries (source_id, guid, title, url, link_key, created_at, updated_at)
			VALUES (?, ?, ?, '', '', ?, ?)`, id, guid, guid, at, at)
		if err != nil {
			t.Fatal(err)
		}
		entryID, _ := res.LastInsertId()
		return entryID
	}
	wanted := add(in.ID, "in")
	add(hidden.ID, "hid")
	add(out.ID, "out")

	list, err := s.Entries(ctx, EntryFilter{FolderID: folder.ID, Kind: "article"}, 100)
	if err != nil || len(list) != 1 || list[0].ID != wanted {
		t.Fatalf("Entries in the folder = %+v, %v; want only %d", list, err, wanted)
	}
}
