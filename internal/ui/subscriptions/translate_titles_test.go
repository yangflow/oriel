package subscriptions

import "testing"

func TestEditTranslateTitles(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		t.Run(language, func(t *testing.T) {
			s, f, tt := setup(t, language)
			f.settings.AIBaseURL, f.settings.AIModel = "https://api.example.com/v1", "small"
			tt.SetSize(1000, 1600)
			s.Edit(f.sources[0])
			tt.Frame()
			f.settle(tt)
			for _, text := range []string{s.text.TranslateTitles, s.text.TranslateTitlesHelp} {
				if !tt.HasText(text) {
					t.Errorf("no %q in %v", text, tt.Texts())
				}
			}
			if s.edit.titles {
				t.Fatal("the switch starts on")
			}
			if err := tt.Click(s.text.TranslateTitlesHelp); err != nil {
				t.Fatal(err)
			}
			if err := tt.Click(s.text.Save); err != nil {
				t.Fatal(err)
			}
			f.settle(tt)
			p := f.patches[1]
			if len(p) != 1 || p[0].TranslateTitles == nil || !*p[0].TranslateTitles || p[0].AutoSummary != nil || p[0].Title != nil {
				t.Fatalf("patches %+v", p)
			}
		})
	}
}

func TestEditLeavesTranslateTitlesAloneWhenUntouched(t *testing.T) {
	s, f, tt := setup(t, "en")
	f.sources[0].TranslateTitles = true
	tt.SetSize(1000, 1600)
	s.Edit(f.sources[0])
	tt.Frame()
	f.settle(tt)
	if !s.edit.titles {
		t.Fatal("the switch does not show the saved value")
	}
	s.edit.title = "Renamed"
	if err := tt.Click(s.text.Save); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if p := f.patches[1]; len(p) != 1 || p[0].TranslateTitles != nil {
		t.Fatalf("patches %+v", p)
	}
}
