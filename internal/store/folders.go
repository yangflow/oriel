package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// MaxFolderName is the longest folder name, in characters.
const MaxFolderName = 60

// ErrInvalidFolderName reports a folder name that is empty after trimming or longer than MaxFolderName.
var ErrInvalidFolderName = errors.New("invalid folder name")

// ErrNoFolder reports a folder id that names no folder.
var ErrNoFolder = errors.New("no such folder")

// FolderExistsError says which folder already has the name. It matches ErrExists.
type FolderExistsError struct{ ID int64 }

func (e *FolderExistsError) Error() string { return "a folder with this name already exists" }

func (e *FolderExistsError) Is(target error) bool { return target == ErrExists }

// Folder groups subscriptions. SourceCount counts the subscriptions in it that are not deleted.
type Folder struct {
	ID          int64
	Name        string
	SourceCount int
	CreatedAt   time.Time
}

// CleanFolderName trims a folder name and returns false when it is empty. A name over MaxFolderName characters is
// cut when cut is set (names read from a file) and refused otherwise (names typed by the user).
func CleanFolderName(name string, cut bool) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false
	}
	if utf8.RuneCountInString(name) > MaxFolderName {
		if !cut {
			return "", false
		}
		name = strings.TrimSpace(string([]rune(name)[:MaxFolderName]))
	}
	return name, name != ""
}

const folderColumns = `f.id, f.name, f.created_at,
	(SELECT count(*) FROM sources s WHERE s.folder_id = f.id AND s.deleted_at IS NULL)`

func scanFolder(row interface{ Scan(...any) error }) (Folder, error) {
	var f Folder
	var created int64
	if err := row.Scan(&f.ID, &f.Name, &created, &f.SourceCount); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Folder{}, ErrNotFound
		}
		return Folder{}, err
	}
	f.CreatedAt = time.UnixMilli(created)
	return f, nil
}

// Folders lists the folders by name, without regard to case.
func (s *Store) Folders(ctx context.Context) ([]Folder, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+folderColumns+" FROM folders f ORDER BY lower(f.name), f.id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	folders := []Folder{}
	for rows.Next() {
		f, err := scanFolder(rows)
		if err != nil {
			return nil, err
		}
		folders = append(folders, f)
	}
	return folders, rows.Err()
}

// Folder returns one folder, or ErrNotFound.
func (s *Store) Folder(ctx context.Context, id int64) (Folder, error) {
	return scanFolder(s.db.QueryRowContext(ctx, "SELECT "+folderColumns+" FROM folders f WHERE f.id = ?", id))
}

// CreateFolder adds a folder. It returns ErrInvalidFolderName for an empty or too long name and a *FolderExistsError,
// which matches ErrExists, when another folder has the name.
func (s *Store) CreateFolder(ctx context.Context, name string, now time.Time) (Folder, error) {
	name, ok := CleanFolderName(name, false)
	if !ok {
		return Folder{}, ErrInvalidFolderName
	}
	result, err := s.db.ExecContext(ctx, "INSERT INTO folders (name, created_at) VALUES (?, ?)", name, now.UnixMilli())
	if err != nil {
		return Folder{}, s.folderConflict(ctx, err, name)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Folder{}, err
	}
	return s.Folder(ctx, id)
}

// RenameFolder changes the name of a folder, with the errors of CreateFolder and ErrNotFound.
func (s *Store) RenameFolder(ctx context.Context, id int64, name string) (Folder, error) {
	name, ok := CleanFolderName(name, false)
	if !ok {
		return Folder{}, ErrInvalidFolderName
	}
	result, err := s.db.ExecContext(ctx, "UPDATE folders SET name = ? WHERE id = ?", name, id)
	if err != nil {
		return Folder{}, s.folderConflict(ctx, err, name)
	}
	if n, err := result.RowsAffected(); err != nil {
		return Folder{}, err
	} else if n == 0 {
		return Folder{}, ErrNotFound
	}
	return s.Folder(ctx, id)
}

// folderConflict turns a failed write of a name into a *FolderExistsError when another folder has the name.
func (s *Store) folderConflict(ctx context.Context, err error, name string) error {
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) && sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE {
		var id int64
		if s.db.QueryRowContext(ctx, "SELECT id FROM folders WHERE name = ?", name).Scan(&id) == nil {
			return &FolderExistsError{ID: id}
		}
		return ErrExists
	}
	return err
}

// DeleteFolder removes a folder. Its subscriptions stay and become unfiled.
func (s *Store) DeleteFolder(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM folders WHERE id = ?", id)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrNotFound
	}
	return nil
}

// EnsureFolder returns the folder with the name, creating it when there is none, and says whether it created it.
// The name is cleaned like one read from a file: trimmed and cut to MaxFolderName characters; an empty one is
// ErrInvalidFolderName.
func (s *Store) EnsureFolder(ctx context.Context, name string, now time.Time) (id int64, created bool, err error) {
	name, ok := CleanFolderName(name, true)
	if !ok {
		return 0, false, ErrInvalidFolderName
	}
	err = s.db.QueryRowContext(ctx, "SELECT id FROM folders WHERE name = ?", name).Scan(&id)
	if err == nil {
		return id, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, false, err
	}
	folder, err := s.CreateFolder(ctx, name, now)
	if err != nil {
		return 0, false, err
	}
	return folder.ID, true, nil
}

// FileSource puts a subscription that is in no folder into a folder; one that is in a folder stays there. It says
// whether it moved the subscription.
func (s *Store) FileSource(ctx context.Context, sourceID, folderID int64) (bool, error) {
	result, err := s.db.ExecContext(ctx,
		"UPDATE sources SET folder_id = ? WHERE id = ? AND folder_id IS NULL AND deleted_at IS NULL", folderID, sourceID)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n > 0, err
}
