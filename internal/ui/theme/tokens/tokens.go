// Package tokens holds the values of Oriel's design system, generated from design/tokens.json: the colors of each
// scheme, the pane and the reading presets, spacing, radii, sizes, opacities, families, text styles, the reading
// scale, motion and the contrast floors. It holds values only; package theme decides how a screen uses them, scales
// the DIPs with the text size and applies high contrast. Edit tokens.json, then run go generate here.
package tokens

//go:generate go run ./gen
