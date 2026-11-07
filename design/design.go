// Package design holds the product assets the program draws itself: the product icon of the connect screen.
package design

import _ "embed"

// Icon is the product icon, the mark on the ivory tile (icon.svg). Its drop shadows are filters, which MyGo leaves out.
//
//go:embed icon.svg
var Icon []byte
