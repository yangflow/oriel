// Package prefs holds the appearance preferences of this device: the theme, the interface language and font, and the
// font, size, line spacing and Chinese typesetting of an article, how the timeline views each kind of entry, whether
// its sidebar shows, and the order of the sidebar's subscriptions. They live in a file of the data directory beside the
// session file and never go through the server.
package prefs

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// File is the name of the preferences file in the data directory.
const File = "preferences.json"

// The choices of each preference. The empty value is the default of Theme, Language and Font: follow the system.
const (
	ThemeLight = "light"
	ThemeDark  = "dark"

	LanguageEnglish = "en"
	LanguageChinese = "zh-Hans"

	// ReadingSerif is the value of ReadingFont that stands for the platform's serif set; any other value is a family.
	ReadingSerif = "serif"

	SizeSmall  = "small"
	SizeMedium = "medium"
	SizeLarge  = "large"
	SizeXLarge = "xlarge"

	SpacingCompact  = "compact"
	SpacingStandard = "standard"
	SpacingRelaxed  = "relaxed"

	// The viewers of the timeline, one set per kind of entry; the first of each set is its default.
	ViewList    = "list"    // an article's row; a video's row
	ViewCompact = "compact" // an article or a social post in a few lines
	ViewGrid    = "grid"    // the cover grid of Videos; the square grid of Images
	ViewCover   = "cover"   // a video as a large cover
	ViewCard    = "card"    // a social post's card; an image's card
	ViewWall    = "wall"    // the masonry wall of Images

	// The orders of the sidebar's subscriptions; the first is the default.
	SortTitle  = "title"  // by name
	SortUnread = "unread" // unread count descending, then by name
)

// Sizes and Spacings list the choices of the reading preferences in the order the settings show them.
var (
	Sizes    = []string{SizeSmall, SizeMedium, SizeLarge, SizeXLarge}
	Spacings = []string{SpacingCompact, SpacingStandard, SpacingRelaxed}

	ArticleViewers = []string{ViewList, ViewCompact}
	VideoViewers   = []string{ViewGrid, ViewCover, ViewList}
	SocialViewers  = []string{ViewCard, ViewCompact}
	ImageViewers   = []string{ViewWall, ViewGrid, ViewCard}
)

// Preferences are the choices of this device. Use Normalize before relying on the reading choices.
type Preferences struct {
	Theme       string `json:"theme,omitempty"`       // "" follows the system, else ThemeLight or ThemeDark
	Language    string `json:"language,omitempty"`    // "" follows the system, else LanguageEnglish or LanguageChinese
	Font        string `json:"font,omitempty"`        // "" is the system font, else an installed family
	ReadingFont string `json:"readingFont,omitempty"` // "" follows the interface font, else ReadingSerif or an installed family
	ReadingSize string `json:"readingSize"`           // one of Sizes
	LineSpacing string `json:"lineSpacing"`           // one of Spacings

	// ChineseTypesetting sets Chinese text in the reader by the rules of docs/design.md. A file without the field
	// reads as on, because Load starts from Default.
	ChineseTypesetting bool `json:"chineseTypesetting"`

	// How the timeline draws each kind of entry: one of ArticleViewers, VideoViewers, SocialViewers and ImageViewers.
	// Use Normalize before relying on them.
	ArticleViewer string `json:"articleViewer"`
	VideoViewer   string `json:"videoViewer"`
	SocialViewer  string `json:"socialViewer"`
	ImageViewer   string `json:"imageViewer"`

	// SourceSort is the order of the subscriptions in the sidebar: SortTitle or SortUnread.
	SourceSort string `json:"sourceSort"`

	// Grids are the scopes whose list the reader switched between the list and the grid; see Grids.
	Grids Grids `json:"grids,omitempty"`

	// SidebarHidden is set while the reader has collapsed the sidebar of the timeline; the default is shown.
	SidebarHidden bool `json:"sidebarHidden,omitempty"`
}

// Grids remembers, per scope, that the list was switched to the grid (ViewGrid) or to the list (ViewList) against what
// the viewers choose. A scope is "kind:video", "kind:image" or "source:<id>"; one that was never switched, or whose
// switch was undone, has no entry, so the set stays small. It is a string inside, so that Preferences stays comparable,
// and a JSON object outside: {"kind:video":"list","source:12":"grid"}.
type Grids string

// GridScope names a scope of the timeline for Grids: the subscription when there is one, else the kind.
func GridScope(kind string, source int64) string {
	if source != 0 {
		return "source:" + strconv.FormatInt(source, 10)
	}
	return "kind:" + kind
}

func validGridScope(scope string) bool {
	switch scope {
	case "kind:video", "kind:image":
		return true
	}
	id, ok := strings.CutPrefix(scope, "source:")
	n, err := strconv.ParseInt(id, 10, 64)
	return ok && err == nil && n > 0 && strconv.FormatInt(n, 10) == id
}

// Get is the switch of a scope: ViewGrid, ViewList, or "" when it was not switched.
func (g Grids) Get(scope string) string {
	for rest := string(g); rest != ""; {
		var entry string
		entry, rest, _ = strings.Cut(rest, ",")
		if key, value, _ := strings.Cut(entry, "="); key == scope {
			return value
		}
	}
	return ""
}

// Set returns g with the scope switched to value, which is dropped instead when it is what the viewers choose anyway,
// def (ViewGrid or ViewList); a scope or value it does not know changes nothing.
func (g Grids) Set(scope, value, def string) Grids {
	if !validGridScope(scope) || (value != ViewGrid && value != ViewList) {
		return g
	}
	m := g.entries()
	if value == def {
		delete(m, scope)
	} else {
		m[scope] = value
	}
	return encodeGrids(m)
}

func (g Grids) entries() map[string]string {
	m := map[string]string{}
	for rest := string(g); rest != ""; {
		var entry string
		entry, rest, _ = strings.Cut(rest, ",")
		if key, value, _ := strings.Cut(entry, "="); validGridScope(key) && (value == ViewGrid || value == ViewList) {
			m[key] = value
		}
	}
	return m
}

func encodeGrids(m map[string]string) Grids {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for i, k := range keys {
		keys[i] = k + "=" + m[k]
	}
	return Grids(strings.Join(keys, ","))
}

// MarshalJSON writes the switches as an object.
func (g Grids) MarshalJSON() ([]byte, error) { return json.Marshal(g.entries()) }

// UnmarshalJSON reads the object and keeps the switches it knows; anything else reads as no switches, so a bad value
// never costs the other preferences.
func (g *Grids) UnmarshalJSON(data []byte) error {
	var m map[string]string
	if json.Unmarshal(data, &m) != nil {
		*g = ""
		return nil
	}
	known := map[string]string{}
	for k, v := range m {
		if validGridScope(k) && (v == ViewGrid || v == ViewList) {
			known[k] = v
		}
	}
	*g = encodeGrids(known)
	return nil
}

// Default has every preference at its default.
func Default() Preferences {
	return Preferences{ReadingSize: SizeMedium, LineSpacing: SpacingStandard, ChineseTypesetting: true,
		ArticleViewer: ViewList, VideoViewer: ViewGrid, SocialViewer: ViewCard, ImageViewer: ViewWall, SourceSort: SortTitle}
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
	if !slices.Contains(ArticleViewers, p.ArticleViewer) {
		p.ArticleViewer = ViewList
	}
	if !slices.Contains(VideoViewers, p.VideoViewer) {
		p.VideoViewer = ViewGrid
	}
	if !slices.Contains(SocialViewers, p.SocialViewer) {
		p.SocialViewer = ViewCard
	}
	if !slices.Contains(ImageViewers, p.ImageViewer) {
		p.ImageViewer = ViewWall
	}
	if p.SourceSort != SortTitle && p.SourceSort != SortUnread {
		p.SourceSort = SortTitle
	}
	if p.Grids != "" {
		p.Grids = encodeGrids(p.Grids.entries())
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
// sizes are about 14.9, 16, 18.1 and 20.3 in every language: the factors are 14/15, 1, 17/15 and 19/15.
func (p Preferences) Scale() float64 {
	switch p.ReadingSize {
	case SizeSmall:
		return 14.0 / 15
	case SizeLarge:
		return 17.0 / 15
	case SizeXLarge:
		return 19.0 / 15
	}
	return 1
}

// Column is the greatest width of the reader's text column, in the units of the mockup: 680 times the Scale, rounded
// to a whole unit, so 635, 680, 771 and 861.
func (p Preferences) Column() float64 { return math.Round(680 * p.Scale()) }

// SystemSerif is how MyGo names New York, the serif face of macOS. It is a system font, not a family the font list
// has, and MyGo finds it by this name and not by "New York".
const SystemSerif = ".AppleSystemUIFontSerif"

// SerifFamilies are the families that make up Serif on a platform (a runtime.GOOS), a Latin face first and a
// Simplified Chinese one behind it.
func SerifFamilies(goos string) []string {
	switch goos {
	case "darwin":
		return []string{SystemSerif, "Songti SC"}
	case "windows":
		return []string{"Georgia", "SimSun"}
	}
	return []string{"Noto Serif", "Noto Serif CJK SC"}
}

// ReadingFace is the font list of an article's body, comma separated as the MyGo Font takes it, or "" when the body is
// drawn in the interface font. goos is the platform and installed the font families of the system, nil until they are
// known; a family chosen but not installed (and a Serif of which no family is) gives "". The interface font, if there
// is one, comes last, for the glyphs the others lack. It reads the list of families: call it when the preferences or
// the families change, not for each text.
func (p Preferences) ReadingFace(goos string, installed []string, interfaceFont string) string {
	var faces []string
	switch p.ReadingFont {
	case "":
		return ""
	case ReadingSerif:
		for _, f := range SerifFamilies(goos) {
			if installed == nil || f == SystemSerif || slices.Contains(installed, f) {
				faces = append(faces, f)
			}
		}
	default:
		if installed == nil || slices.Contains(installed, p.ReadingFont) {
			faces = append(faces, p.ReadingFont)
		}
	}
	if len(faces) == 0 {
		return ""
	}
	if interfaceFont != "" {
		faces = append(faces, interfaceFont)
	}
	return strings.Join(faces, ",")
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
