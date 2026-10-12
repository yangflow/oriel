// Package example demonstrates a native view with state owned by the UI thread.
package example

import (
	_ "embed"
	"encoding/json"
	"strconv"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/ui/locale"
)

//go:embed copy.json
var catalog []byte

type copy struct{ Title, Increment, Decrement, Reset, Hint string }

var translations = func() map[string]copy {
	var values map[string]copy
	if err := json.Unmarshal(catalog, &values); err != nil {
		panic(err)
	}
	return values
}()

// Screen keeps only the in-memory state of the example.
type Screen struct {
	count int
	text  copy
}

// New selects the best available translation, falling back to English.
func New(systemLocale string) *Screen {
	return &Screen{text: translations[locale.Language(systemLocale)]}
}

// View handles input and draws the current count, without disk or network work.
// Spacing and type sizes derive from the theme so they follow its Spacing, FontSize and the
// desktop's text scale; see docs/design.md.
func (s *Screen) View(c *ui.Context) {
	before := s.count
	t := c.Theme()
	if c.Shortcut(0, ui.KeyUp) {
		s.count++
	}
	if c.Shortcut(0, ui.KeyDown) {
		s.count--
	}
	ui.Column(c).Fill().Center().Gap(t.Space(4)).Padding(t.Space(6)).Children(func() {
		ui.Text(c, s.text.Title).FontSize(t.Rem(1.8)).Bold().Role(ui.RoleHeading).Level(1)
		ui.Textf(c, "%d", s.count).FontSize(t.Rem(3.6)).Bold()
		ui.Row(c).Gap(t.Space(2)).Children(func() {
			if ui.Button(c, "−").Label(s.text.Decrement).Width(t.Space(11)).Clicked() {
				s.count--
			}
			if ui.Button(c, s.text.Reset).Disabled(s.count == 0).Clicked() {
				s.count = 0
			}
			if ui.PrimaryButton(c, "+").Label(s.text.Increment).Width(t.Space(11)).Clicked() {
				s.count++
			}
		})
		ui.Text(c, s.text.Hint).FontSize(t.Rem(0.9)).TextColor(t.TextMuted)
	})
	if s.count != before {
		// The count is not focusable, so tell screen readers about the change.
		c.Announce(strconv.Itoa(s.count))
	}
}
