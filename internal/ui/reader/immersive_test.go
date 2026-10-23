package reader

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestImmersiveKeys(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		t.Run(language, func(t *testing.T) {
			s, f, tt := setup(t, language, full())
			f.settle(tt)
			tt.Key(0, ui.KeyZ)
			if !slices.Equal(f.calls, []string{"immerse"}) {
				t.Fatalf("Z: calls %v", f.calls)
			}
			// The timeline owns the state: with it on, Escape leaves it and does not go back.
			f.calls = nil
			f.nav.Immersive = true
			tt.Frame()
			tt.Key(0, ui.KeyEscape)
			if !slices.Equal(f.calls, []string{"immerse"}) {
				t.Fatalf("Escape in immersive reading: calls %v", f.calls)
			}
			f.calls = nil
			f.nav.Immersive = false
			tt.Frame()
			tt.Key(0, ui.KeyEscape)
			if !slices.Equal(f.calls, []string{"back"}) {
				t.Fatalf("Escape: calls %v", f.calls)
			}
			if _, ok := tt.Find(s.text.ExitImmersive); ok {
				t.Fatal("the exit button shows outside immersive reading")
			}
		})
	}
}

func TestImmersiveEscapeClosesSelectionFirst(t *testing.T) {
	_, f, tt := tall(t, "en")
	f.settle(tt)
	f.selReply = "An answer."
	askAbout(t, tt, "Translate selection")
	f.settle(tt)
	f.nav.Immersive = true
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	absent(t, tt, "An answer.")
	if len(f.calls) != 0 {
		t.Fatalf("calls %v: Escape left immersive reading with the card open", f.calls)
	}
	tt.Key(0, ui.KeyEscape)
	if !slices.Equal(f.calls, []string{"immerse"}) {
		t.Fatalf("calls %v", f.calls)
	}
}

func TestImmersiveExitButton(t *testing.T) {
	s, f, tt := setup(t, "en", full())
	f.settle(tt)
	f.nav.Immersive = true
	tt.Frame()
	if _, ok := tt.Find(s.text.ExitImmersive); !ok {
		t.Fatal("no exit button")
	}
	click(t, tt, s.text.ExitImmersive)
	if !slices.Equal(f.calls, []string{"immerse"}) {
		t.Fatalf("calls %v", f.calls)
	}
	// J, K, S and the rest work as ever.
	f.calls = nil
	tt.Key(0, ui.KeyJ)
	tt.Key(0, ui.KeyK)
	if !slices.Equal(f.calls, []string{"next", "previous"}) {
		t.Fatalf("calls %v", f.calls)
	}
}
