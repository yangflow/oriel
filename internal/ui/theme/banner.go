package theme

import (
	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/ui/icons"
	"github.com/yangflow/oriel/internal/ui/theme/tokens"
)

// Banner is a status that stays until its cause goes away, design CBanner: one line on a surface plate, radius-card,
// padding space-3, the icon in a control square of the fill carrying the tone, the title in label over the text in
// meta, and the actions at the end. Only the glyph takes the tone; the words stay in ink.
type Banner struct {
	Icon *ui.SVG
	// Tone colors the glyph: Accent for news, Warning, Danger or Success.
	Tone ui.Color
	// Busy turns a loader in place of the icon.
	Busy        bool
	Title, Text string
	// Actions draws the small buttons at the end, the main one Secondary and last.
	Actions func()
}

// BannerHeight is the height of a Banner in mockup pixels: a control square and the plate's padding.
const BannerHeight = tokens.Control + 2*tokens.Space3

// Draw builds the banner and returns its plate, a status for screen readers.
func (bn Banner) Draw(c *ui.Context) ui.Element {
	t := c.Theme()
	plate := ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(Px(t, tokens.Space3)).Padding(Px(t, tokens.Space3)).
		MinHeight(Px(t, BannerHeight)).Radius(Px(t, tokens.RadiusCard)).Background(Surface(t)).Role(ui.RoleStatus)
	plate.Children(func() {
		square := Px(t, tokens.Control)
		ui.Box(c).Size(square, square).Shrink(0).Radius(Px(t, tokens.RadiusControl)).Center().Background(Track(t)).
			Children(func() {
				if bn.Busy {
					spin := ui.Icon(c, icons.LoaderCircle).FontSize(IconMedium(t)).TextColor(Text2(t))
					spin.Rotate(spin.Loop("spin", tokens.MotionSpin, ui.Linear) * 360)
					return
				}
				ui.Icon(c, bn.Icon).FontSize(IconMedium(t)).TextColor(bn.Tone)
			})
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, bn.Title).FontWeight(500).SingleLine()
			if bn.Text != "" {
				ui.Text(c, bn.Text).FontSize(Px(t, tokens.TextMeta.Size.En)).FontFeatures("tnum").TextColor(Text2(t)).
					SingleLine()
			}
		})
		if bn.Actions != nil {
			ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(Px(t, tokens.Space2)).Children(bn.Actions)
		}
	})
	return plate
}
