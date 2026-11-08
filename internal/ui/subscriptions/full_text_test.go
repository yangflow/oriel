package subscriptions

import (
	"testing"

	"github.com/yangflow/oriel/internal/client"
)

// The Fetch full text switch applies at once and sends only itself; only an article subscription has it.
func TestPageFullText(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		t.Run(language, func(t *testing.T) {
			s, f, tt := paged(t, language, 0, func(src *client.Source) { src.Kind, src.FullText = "article", true })
			if !tt.HasText(s.text.FullText) || !s.pg.e.fullText {
				t.Fatalf("no switch, or off: %v", tt.Texts())
			}
			before := len(f.patches[1])
			s.pg.e.fullText = false // as the switch does
			tt.Frame()
			f.settle(tt)
			p := f.patches[1]
			if len(p) != before+1 || p[before].FullText == nil || *p[before].FullText || p[before].TranslateTitles != nil {
				t.Fatalf("patches %+v", p)
			}
		})
	}
	s, _, tt := paged(t, "en", 0, func(src *client.Source) { src.Kind = "video" })
	if tt.HasText(s.text.FullText) {
		t.Fatal("a video subscription offers the full text")
	}
}
