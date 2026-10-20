// Package prefs holds the appearance preferences of this device: the theme, the interface language and font, and the
// size and line spacing of an article. They live in a file of the data directory beside the session file and never
// go through the server.
package prefs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
)

// File is the name of the preferences file in the data directory.
const File = "preferences.json"

// The choices of each preference. The empty value is the default of Theme, Language and Font: follow the system.
const (
	ThemeLight = "light"
	ThemeDark  = "dark"

	LanguageEnglish = "en"
	LanguageChinese = "zh-Hans"

	SizeSmall  = "small"
	SizeMedium = "medium"
	SizeLarge  = "large"
	SizeXLarge = "xlarge"

	SpacingCompact  = "compact"
	SpacingStandard = "standard"
	SpacingRelaxed  = "relaxed"
)

// Sizes and Spacings list the choices of the reading preferences in the order the settings show them.
var (
	Sizes    = []string{SizeSmall, SizeMedium, SizeLarge, SizeXLarge}
	Spacings = []string{SpacingCompact, SpacingStandard, SpacingRelaxed}
)

// Preferences are the choices of this device. Use Normalize before relying on the reading choices.
type Preferences struct {
	Theme       string `json:"theme,omitempty"`    // "" follows the system, else ThemeLight or ThemeDark
	Language    string `json:"language,omitempty"` // "" follows the system, else LanguageEnglish or LanguageChinese
	Font        string `json:"font,omitempty"`     // "" is the system font, else an installed family
	ReadingSize string `json:"readingSize"`        // one of Sizes
	LineSpacing string `json:"lineSpacing"`        // one of Spacings
}

// Default has every preference at its default.
func Default() Preferences {
	return Preferences{ReadingSize: SizeMedium, LineSpacing: SpacingStandard}
}

// Normalize replaces every value it does not know with the default. A font is kept as it is: it may be installed
// again later.
func (p Preferences) Normalize() Preferences {
	if p.Theme != ThemeLight && p.Theme != ThemeDark {
		p.Theme = ""
	}
	if p.Language != LanguageEnglish && p.Language != LanguageChinese {
		p.Language = ""
	}
	if !slices.Contains(Sizes, p.ReadingSize) {
		p.ReadingSize = SizeMedium
	}
	if !slices.Contains(Spacings, p.LineSpacing) {
		p.LineSpacing = SpacingStandard
	}
	return p
}

// Dark tells whether the theme is dark, given whether the system is.
func (p Preferences) Dark(system bool) bool {
	switch p.Theme {
	case ThemeLight:
		return false
	case ThemeDark:
		return true
	}
	return system
}

// Scale is the factor of an article's sizes: 1 for the medium text size, whose body is 16. The bodies of the four
// sizes are 14.9, 16, 18.1 and 20.3 in every language.
func (p Preferences) Scale() float64 {
	switch p.ReadingSize {
	case SizeSmall:
		return 14.9 / 16
	case SizeLarge:
		return 18.1 / 16
	case SizeXLarge:
		return 20.3 / 16
	}
	return 1
}

// Leading is the line height of an article's paragraphs; han is for Simplified Chinese text, which needs more.
func (p Preferences) Leading(han bool) float64 {
	i := 1
	switch p.LineSpacing {
	case SpacingCompact:
		i = 0
	case SpacingRelaxed:
		i = 2
	}
	if han {
		return [...]float64{1.7, 1.85, 2.0}[i]
	}
	return [...]float64{1.55, 1.7, 1.85}[i]
}

// Load reads the preferences. A missing or unreadable file gives the defaults, and so does any value it does not
// know.
func Load(path string) Preferences {
	data, err := os.ReadFile(path)
	if err != nil {
		return Default()
	}
	p := Default()
	if json.Unmarshal(data, &p) != nil {
		return Default()
	}
	return p.Normalize()
}

// Save writes the preferences readable by the user only, replacing the file at once.
func Save(path string, p Preferences) error {
	data, err := json.Marshal(p.Normalize())
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".preferences-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
