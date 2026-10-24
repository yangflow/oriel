package tracker

import (
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// Counts reports whether an img element, with the address src it would load, only counts visits and is no part of
// the article: a picture of a pixel or two, a hidden one, or one of a known tracker.
func Counts(n *html.Node, src string) bool {
	width, _ := strconv.Atoi(attr(n, "width"))
	height, _ := strconv.Atoi(attr(n, "height"))
	return width > 0 && width <= 2 || height > 0 && height <= 2 || Hidden(n) || KnownAddress(src)
}

// attr is the value of the attribute key of n, "" when it has none.
func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// Hidden reports whether an element is kept from view by its attributes or its inline style: hidden, display none,
// visibility hidden, or a width or height of nothing or a pixel.
func Hidden(n *html.Node) bool {
	for _, a := range n.Attr {
		if a.Key == "hidden" {
			return true
		}
	}
	for _, rule := range strings.Split(strings.ToLower(attr(n, "style")), ";") {
		name, value, ok := strings.Cut(rule, ":")
		if !ok {
			continue
		}
		name, value = strings.TrimSpace(name), strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(value), "!important"))
		switch {
		case name == "display" && value == "none", name == "visibility" && value == "hidden":
			return true
		case name == "width" || name == "height" || name == "max-width" || name == "max-height":
			value = strings.TrimSuffix(value, "px")
			if size, err := strconv.ParseFloat(value, 64); err == nil && size <= 1 {
				return true
			}
		}
	}
	return false
}
