package subscriptions

import "testing"

// The edit dialog is four groups in a fixed order, with Pause fetching first in Fetching and no help line under a
// closed Extraction.
func TestEditDialogGroups(t *testing.T) {
	for _, lang := range []string{"en", "zh-Hans"} {
		s, f, tt := setup(t, lang)
		tt.SetSize(1000, 2400)
		s.Edit(f.sources[0])
		tt.Frame()
		f.settle(tt)
		want := []string{
			s.text.GroupShow, s.text.GroupFetch, s.text.PauseLabel, s.text.Extract,
			s.text.GroupTimeline, s.text.Keep, s.text.Pace, s.text.Mute, s.text.AIGroup,
		}
		at := 0
		for _, text := range tt.Texts() {
			if at < len(want) && text == want[at] {
				at++
			}
		}
		if at < len(want) {
			t.Fatalf("%s: order stopped at %q in %v", lang, want[at], tt.Texts())
		}
		if tt.HasText(s.text.ExtractHelp) {
			t.Fatalf("%s: help of the closed Extraction shows", lang)
		}
	}
}
