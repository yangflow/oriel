package icons

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestEveryIconParses(t *testing.T) {
	names, err := fs.Glob(files, "lucide/*.svg")
	if err != nil || len(names) == 0 {
		t.Fatalf("no icons: %v", err)
	}
	for _, name := range names {
		data, _ := files.ReadFile(name)
		if !strings.HasPrefix(string(data), "<!-- @license lucide-static v1.59.0 - ISC -->") {
			t.Errorf("%s: not the unchanged lucide-static file", name)
		}
		s, err := ui.ParseSVG(data)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if w, h := s.Size(); w != 24 || h != 24 {
			t.Errorf("%s: size %vx%v", name, w, h)
		}
	}
}
