package store

import (
	"encoding/binary"
	"errors"
	"io/fs"
	"os"
)

// userVersionOffset is where a SQLite file header keeps PRAGMA user_version: four bytes, big endian.
const userVersionOffset = 60

// PreMigrateCopy describes the copy backupBeforeMigrate left beside the database.
type PreMigrateCopy struct {
	Bytes   int64 // the size of the file
	Version int   // the schema version the copy was made from, 0 when the header cannot be read
}

// PreMigrateInfo reports the copy made before the last migration, or nil when there is none. The version comes from the
// copy's own file header, which VACUUM INTO keeps, so the file is only read, never opened as a database. Nothing about
// the location leaves this function.
func (s *Store) PreMigrateInfo() (*PreMigrateCopy, error) {
	file := s.path + PreMigrateSuffix
	info, err := os.Stat(file)
	if errors.Is(err, fs.ErrNotExist) || err == nil && !info.Mode().IsRegular() {
		return nil, nil
	}
	if err != nil {
		return nil, withoutDirectory(err)
	}
	found := &PreMigrateCopy{Bytes: info.Size()}
	f, err := os.Open(file)
	if err != nil {
		return nil, withoutDirectory(err)
	}
	defer f.Close()
	var raw [4]byte
	if _, err := f.ReadAt(raw[:], userVersionOffset); err == nil {
		found.Version = int(binary.BigEndian.Uint32(raw[:]))
	}
	return found, nil
}

// DeletePreMigrate removes the copy made before the last migration. A copy that is already gone is not an error.
func (s *Store) DeletePreMigrate() error {
	file := s.path + PreMigrateSuffix
	if info, err := os.Stat(file); err == nil && !info.Mode().IsRegular() {
		return nil // not a copy of ours; leave it
	}
	if err := os.Remove(file); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return withoutDirectory(err)
	}
	return nil
}
