package main

import (
	"path/filepath"
	"strings"
)

// saveTypes returns the file extensions a save dialog accepts for the suggested file name: the first one is the
// extension the file gets when the name has none. OPML also accepts .xml.
func saveTypes(name string) []string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md":
		return []string{"md"}
	case ".txt":
		return []string{"txt"}
	case ".oriel-backup":
		return []string{"oriel-backup"}
	}
	return []string{"opml", "xml"}
}

// saveName is the name the save dialog is pre-filled with. macOS adds the first allowed extension to a name by itself,
// and for an extension it does not know (opml, oriel-backup) it does so even when the name already ends with it
// (Oriel.opml.opml), so there the extension is left off and the panel adds it once. Windows adds the extension of the
// chosen file type too, but Linux (GTK) adds nothing, so everywhere else the name keeps its extension.
func saveName(name, goos string) string {
	if goos != "darwin" {
		return name
	}
	return strings.TrimSuffix(name, filepath.Ext(name))
}

// savePath makes sure the file the user picked ends in one of the accepted extensions, which a dialog that adds
// nothing (GTK) or a name the user cleared does not guarantee; a path with another extension keeps it and gets the
// first accepted one after it, so Notes.v2 becomes Notes.v2.opml.
func savePath(path string, types []string) string {
	if path == "" {
		return path
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	for _, t := range types {
		if ext == t {
			return path
		}
	}
	return path + "." + types[0]
}
