package startup

import (
	"errors"
	"strings"
	"testing"

	"github.com/yangflow/oriel/internal/ui/locale"
)

func TestCatalogIsComplete(t *testing.T) {
	for _, language := range locale.Languages {
		c := translations[language]
		if c.Title == "" || c.Locked == "" || c.Failed == "" {
			t.Errorf("%s: incomplete copy", language)
		}
		if language != "en" && c.Locked == translations["en"].Locked {
			t.Errorf("%s: untranslated", language)
		}
	}
	if len(translations) != len(locale.Languages) {
		t.Errorf("catalog has %d languages, want %d", len(translations), len(locale.Languages))
	}
}

func TestMessage(t *testing.T) {
	title, detail := Message("zh-CN", true, errors.New("locked"))
	if title != "Oriel 无法启动" || !strings.Contains(detail, "另一个 Oriel 进程") {
		t.Errorf("locked: %q %q", title, detail)
	}
	_, detail = Message("de-AT", false, errors.New("disk full"))
	if !strings.HasPrefix(detail, "Oriel couldn't open") || !strings.HasSuffix(detail, "disk full") {
		t.Errorf("other: %q", detail)
	}
}
