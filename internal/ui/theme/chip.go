package theme

import (
	"github.com/egoist/mygo/ui"

	"github.com/yangflow/oriel/internal/ui/theme/tokens"
)

// RowChip is the icon square at the start of a setting row, design CSettingRow: control square, radius-control, the
// paper fill, the glyph icon-m in Text2, or Danger for a row that destroys.
func RowChip(c *ui.Context, icon *ui.SVG, danger bool) {
	t := c.Theme()
	square := Px(t, tokens.Control)
	color := Text2(t)
	if danger {
		color = t.Danger
	}
	ui.Box(c).Size(square, square).Shrink(0).Radius(Px(t, tokens.RadiusControl)).Center().Background(t.Background).
		Children(func() { ui.Icon(c, icon).FontSize(IconMedium(t)).TextColor(color) })
}
