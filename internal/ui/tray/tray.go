// Package tray holds the words of the menu bar or tray icon and the Dock badge, and how an unread count reads there.
package tray

import (
	_ "embed"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/yangflow/oriel/internal/ui/locale"
)

//go:embed copy.json
var catalog []byte

type copy struct{ Unread, None, Tooltip, Open, Refresh, Quit string }

var translations = func() map[string]copy {
	var values map[string]copy
	if err := json.Unmarshal(catalog, &values); err != nil {
		panic(err)
	}
	return values
}()

// Words are the labels of the tray menu in one language.
type Words struct{ Open, Refresh, Quit string }

// Menu returns the labels of the tray menu in the language, which is an interface language.
func Menu(language string) Words {
	c := translations[locale.Language(language)]
	return Words{Open: c.Open, Refresh: c.Refresh, Quit: c.Quit}
}

// Count is the unread count as the badge and the menu bar title show it: nothing for none, "999+" above 999.
func Count(n int) string {
	switch {
	case n <= 0:
		return ""
	case n > 999:
		return "999+"
	}
	return strconv.Itoa(n)
}

// Unread is the first line of the tray menu.
func Unread(language string, n int) string {
	c := translations[locale.Language(language)]
	if n <= 0 {
		return c.None
	}
	return strings.ReplaceAll(c.Unread, "{n}", Count(n))
}

// Tooltip is what hovering over the icon says.
func Tooltip(language string, n int) string {
	if n <= 0 {
		return "Oriel"
	}
	return strings.ReplaceAll(translations[locale.Language(language)].Tooltip, "{n}", Count(n))
}
