package main

import (
	"slices"
	"testing"
)

func TestSaveTypes(t *testing.T) {
	for name, want := range map[string][]string{
		"Oriel.opml":                    {"opml", "xml"},
		"Oriel-2026-10-25.oriel-backup": {"oriel-backup"},
		"diagnostics.txt":               {"txt"},
		"Article.md":                    {"md"},
	} {
		if got := saveTypes(name); !slices.Equal(got, want) {
			t.Errorf("saveTypes(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestSaveName(t *testing.T) {
	if got := saveName("Oriel.opml", "darwin"); got != "Oriel" {
		t.Errorf("darwin = %q, want the panel to add the extension once", got)
	}
	for _, goos := range []string{"windows", "linux"} {
		if got := saveName("Oriel.opml", goos); got != "Oriel.opml" {
			t.Errorf("%s = %q, want the extension kept", goos, got)
		}
	}
}

func TestSavePath(t *testing.T) {
	opml := []string{"opml", "xml"}
	for _, c := range []struct {
		path  string
		types []string
		want  string
	}{
		{"", opml, ""},
		{"/x/Oriel.opml", opml, "/x/Oriel.opml"},
		{"/x/Feeds.XML", opml, "/x/Feeds.XML"},
		{"/x/Oriel", opml, "/x/Oriel.opml"},
		{"/x/Notes.v2", opml, "/x/Notes.v2.opml"},
		{"/x/Oriel-1", []string{"oriel-backup"}, "/x/Oriel-1.oriel-backup"},
	} {
		if got := savePath(c.path, c.types); got != c.want {
			t.Errorf("savePath(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}
