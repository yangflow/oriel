// Command gen writes tokens_gen.go from design/tokens.json, so the values of Oriel's design system exist in Go exactly
// as the design files hold them. Run it with go generate in internal/ui/theme/tokens; -out - prints instead of writing.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/format"
	"log"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

func main() {
	in := flag.String("in", "../../../../design/tokens.json", "the tokens file")
	out := flag.String("out", "tokens_gen.go", "the Go file to write, or - for standard output")
	flag.Parse()
	data, err := os.ReadFile(*in)
	if err != nil {
		log.Fatal(err)
	}
	src, err := render(data)
	if err != nil {
		log.Fatal(err)
	}
	if *out == "-" {
		_, err = os.Stdout.Write(src)
	} else {
		err = os.WriteFile(*out, src, 0o644)
	}
	if err != nil {
		log.Fatal(err)
	}
}

type scheme struct{ Light, Dark json.RawMessage }

type colorToken struct {
	Name  string
	MyGo  string `json:"mygo"`
	Value scheme
	Usage string
}

type lang struct{ En, Zh float64 }

// UnmarshalJSON reads a number used for both languages or an {"en", "zh"} pair.
func (l *lang) UnmarshalJSON(b []byte) error {
	var n float64
	if json.Unmarshal(b, &n) == nil {
		l.En, l.Zh = n, n
		return nil
	}
	var s string
	if json.Unmarshal(b, &s) == nil {
		n, err := strconv.ParseFloat(strings.TrimSuffix(s, "em"), 64)
		l.En, l.Zh = n, n
		return err
	}
	var p struct{ En, Zh float64 }
	err := json.Unmarshal(b, &p)
	l.En, l.Zh = p.En, p.Zh
	return err
}

type named struct {
	Name     string
	MyGo     string `json:"mygo"`
	Value    json.RawMessage
	Duration float64
	Curve    string
	Usage    string
}

type file struct {
	Color struct {
		Ink    map[string]string
		Tokens []colorToken
		Pane   struct {
			Ink    map[string]string
			Tokens []colorToken
		}
		Presets      map[string]json.RawMessage
		HighContrast struct {
			Overrides []struct{ Name, Value string }
		} `json:"high-contrast"`
	}
	Audit struct {
		Beds    map[string][]string
		Targets []struct {
			Token string
			Min   float64
			On    string
		}
	}
	Shadow  []named
	Opacity struct{ Tokens []named }
	Space   struct{ Tokens []named }
	Radius  struct{ Tokens []named }
	Size    struct{ Tokens []named }
	Font    struct{ Families []named }
	Text    struct {
		Styles []struct {
			Name     string
			Size     json.RawMessage
			Line     json.RawMessage
			Weight   int
			Tracking *lang
			Mono     bool
			Usage    string
		}
	}
	Reading struct {
		Body, H2, H3, Code struct {
			Size     float64
			Line     lang
			Weight   int
			Tracking *lang
			Mono     bool
		}
		SizeScale   map[string]float64 `json:"size-scale"`
		LineSpacing map[string]lang    `json:"line-spacing"`
		LineWidth   map[string]float64 `json:"line-width"`
		Paragraph   lang
		ListItem    lang `json:"list-item"`
		H2Space     struct {
			Above, Below lang
		} `json:"h2-space"`
		HanLatin               lang `json:"han-latin"`
		PunctuationCompression lang `json:"punctuation-compression"`
	}
	Motion struct{ Tokens []named }
}

type writer struct{ bytes.Buffer }

func (w *writer) p(format string, a ...any) { fmt.Fprintf(&w.Buffer, format+"\n", a...) }

// ident turns a token name into a Go identifier: accent-hover is AccentHover, space-0_5 is Space0_5, radius-xs is
// RadiusXS.
func ident(name string) string {
	var b strings.Builder
	for _, part := range strings.Split(name, "-") {
		switch part {
		case "xs", "ui":
			b.WriteString(strings.ToUpper(part))
		default:
			b.WriteString(strings.ToUpper(part[:1]) + part[1:])
		}
	}
	return b.String()
}

// comment folds a usage line into Go comment lines.
func (w *writer) comment(indent, name, usage string) {
	if usage == "" {
		return
	}
	text := name + ": " + usage
	line := indent + "//"
	for _, word := range strings.Fields(text) {
		if len(line)+1+len(word) > 118 {
			w.p("%s", line)
			line = indent + "//"
		}
		line += " " + word
	}
	w.p("%s", line)
}

var rgba = regexp.MustCompile(`^rgba\((\d+),(\d+),(\d+),([.\d]+)\)$`)

// color is the Go expression of a CSS color: #rrggbb or rgba(r,g,b,a).
func color(raw json.RawMessage) (string, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", err
	}
	s = strings.ReplaceAll(s, " ", "")
	if strings.HasPrefix(s, "#") && len(s) == 7 {
		return fmt.Sprintf("ui.Hex(%q)", s), nil
	}
	if m := rgba.FindStringSubmatch(s); m != nil {
		a, err := strconv.ParseFloat(m[4], 64)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("ui.RGBA(%s, %s, %s, %s)", m[1], m[2], m[3], num(a)), nil
	}
	return "", fmt.Errorf("not a color: %s", s)
}

func num(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func number(raw json.RawMessage) (float64, error) {
	var f float64
	if json.Unmarshal(raw, &f) == nil {
		return f, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0, err
	}
	return strconv.ParseFloat(s, 64)
}

func render(data []byte) ([]byte, error) {
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	w := &writer{}
	w.p("// Code generated by go run ./gen from design/tokens.json; DO NOT EDIT.\n")
	w.p("package tokens\n")
	w.p("import (\n\"time\"\n\n\"github.com/egoist/mygo/ui\"\n\"github.com/yangflow/oriel/internal/prefs\"\n)\n")
	if err := w.colors(&f); err != nil {
		return nil, err
	}
	if err := w.presets(&f); err != nil {
		return nil, err
	}
	w.audit(&f)
	if err := w.metrics(&f); err != nil {
		return nil, err
	}
	if err := w.fonts(&f); err != nil {
		return nil, err
	}
	if err := w.text(&f); err != nil {
		return nil, err
	}
	w.reading(&f)
	if err := w.motion(&f); err != nil {
		return nil, err
	}
	return format.Source(w.Bytes())
}

// palette writes one struct type for a set of color tokens, its light and dark values, Named and SetRoles.
func (w *writer) palette(typ, doc string, toks []colorToken, extra func(colorToken) (string, bool)) error {
	w.p("// %s", doc)
	w.p("type %s struct {", typ)
	for _, t := range toks {
		w.comment("\t", ident(t.Name), t.Usage)
		goType := "ui.Color"
		if _, ok := extra(t); ok {
			goType = "float32"
		}
		w.p("\t%s %s", ident(t.Name), goType)
	}
	w.p("}\n")
	for _, s := range []string{"Light", "Dark"} {
		w.p("// %s%s is the %s scheme.", typ, s, strings.ToLower(s))
		w.p("var %s%s = %s{", typ, s, typ)
		for _, t := range toks {
			raw := t.Value.Light
			if s == "Dark" {
				raw = t.Value.Dark
			}
			if _, ok := extra(t); ok {
				v, err := number(raw)
				if err != nil {
					return fmt.Errorf("%s: %w", t.Name, err)
				}
				w.p("\t%s: %s,", ident(t.Name), num(v))
				continue
			}
			c, err := color(raw)
			if err != nil {
				return fmt.Errorf("%s: %w", t.Name, err)
			}
			w.p("\t%s: %s,", ident(t.Name), c)
		}
		w.p("}\n")
	}
	w.p("// Named returns the color of the token called name, and false when it has none.")
	w.p("func (p *%s) Named(name string) (ui.Color, bool) {", typ)
	w.p("switch name {")
	for _, t := range toks {
		if _, ok := extra(t); ok {
			continue
		}
		w.p("case %q:\nreturn p.%s, true", t.Name, ident(t.Name))
	}
	w.p("}\nreturn ui.Color{}, false\n}\n")
	w.p("// SetRoles sets the roles of MyGo's theme that these tokens stand for, leaving every other field as it is.")
	w.p("func (p *%s) SetRoles(t *ui.Theme) {", typ)
	for _, t := range toks {
		if t.MyGo != "" {
			w.p("t.%s = p.%s", t.MyGo, ident(t.Name))
		}
	}
	w.p("}\n")
	return nil
}

func (w *writer) colors(f *file) error {
	soft := func(t colorToken) (string, bool) { return "", t.Name == "soft" }
	none := func(colorToken) (string, bool) { return "", false }
	if err := w.palette("Scheme", "Scheme is every color token of the main window in one scheme. Soft is an alpha: the tint of a meaning color behind its icon.", f.Color.Tokens, soft); err != nil {
		return err
	}
	if err := w.palette("Pane", "Pane is the ink pane's colors in one scheme of the main window; the pane is dark in both.", f.Color.Pane.Tokens, none); err != nil {
		return err
	}
	w.p("// The inks every alpha fill is made of: the main window's and the pane's, light and dark.")
	w.p("var (")
	for _, k := range []struct{ name, val string }{
		{"InkLight", f.Color.Ink["light"]}, {"InkDark", f.Color.Ink["dark"]},
		{"PaneInkLight", f.Color.Pane.Ink["light"]}, {"PaneInkDark", f.Color.Pane.Ink["dark"]},
	} {
		c, err := color(json.RawMessage(strconv.Quote(k.val)))
		if err != nil {
			return err
		}
		w.p("%s = %s", k.name, c)
	}
	w.p(")\n")
	w.p("// StateAlphas are the alphas of the state ladder, the scheme's ink at each.")
	w.p("type StateAlphas struct{ Fill, Hover, Selected, Pressed float32 }\n")
	w.p("// HighContrast is the state ladder with high contrast on. The overrides that are rules, not numbers, are in the")
	w.p("// hand-written theme:")
	var alphas []string
	for _, o := range f.Color.HighContrast.Overrides {
		if v, err := strconv.ParseFloat(o.Value, 64); err == nil {
			alphas = append(alphas, fmt.Sprintf("%s: %s", ident(o.Name), num(v)))
		} else {
			w.p("//   - %s: %s", o.Name, o.Value)
		}
	}
	w.p("var HighContrast = StateAlphas{%s}\n", strings.Join(alphas, ", "))
	return nil
}

func (w *writer) presets(f *file) error {
	names := make([]string, 0, len(f.Color.Presets))
	var fields []string
	seen := map[string]bool{}
	values := map[string]map[string]scheme{}
	for name, raw := range f.Color.Presets {
		if name == "note" {
			continue
		}
		var p map[string]scheme
		if err := json.Unmarshal(raw, &p); err != nil {
			return fmt.Errorf("preset %s: %w", name, err)
		}
		names = append(names, name)
		values[name] = p
		for k := range p {
			if !seen[k] {
				seen[k] = true
				fields = append(fields, k)
			}
		}
	}
	sort.Strings(names)
	order := map[string]int{}
	for i, t := range f.Color.Tokens {
		order[t.Name] = i + 1
	}
	sort.Slice(fields, func(i, j int) bool { return order[fields[i]] < order[fields[j]] })
	w.p("// Preset is a reading appearance preset in one scheme: the page region and the roles drawn on it, in the reader's")
	w.p("// content area only. Ink is what its alpha fills are made of, at the main ladder's alphas.")
	w.p("type Preset struct {")
	for _, k := range fields {
		w.p("\t%s ui.Color", ident(k))
	}
	w.p("}\n")
	w.p("// PresetNames are the presets in tokens.json, in order of name.")
	w.p("var PresetNames = %#v\n", names)
	w.p("// Presets holds each preset's light and dark scheme by name.")
	w.p("var Presets = map[string][2]Preset{")
	for _, n := range names {
		w.p("%q: {", n)
		for _, s := range []string{"light", "dark"} {
			w.p("{")
			for _, k := range fields {
				v, ok := values[n][k]
				if !ok {
					return fmt.Errorf("preset %s has no %s", n, k)
				}
				raw := v.Light
				if s == "dark" {
					raw = v.Dark
				}
				c, err := color(raw)
				if err != nil {
					return fmt.Errorf("preset %s %s: %w", n, k, err)
				}
				w.p("%s: %s,", ident(k), c)
			}
			w.p("},")
		}
		w.p("},")
	}
	w.p("}\n")
	w.p("// Named returns the color of the token called name, and false when the preset has none.")
	w.p("func (p *Preset) Named(name string) (ui.Color, bool) {\nswitch name {")
	for _, k := range fields {
		w.p("case %q:\nreturn p.%s, true", k, ident(k))
	}
	w.p("}\nreturn ui.Color{}, false\n}\n")
	return nil
}

func (w *writer) audit(f *file) {
	w.p("// Target is a contrast floor: Token on every bed of its region, or on the token On when On is set.")
	w.p("type Target struct {\nToken string\nMin float64\nOn string\n}\n")
	w.p("// AuditTargets are the WCAG 2.x contrast floors every text token keeps. A token named pane.x is the pane's x.")
	w.p("var AuditTargets = []Target{")
	for _, t := range f.Audit.Targets {
		w.p("{%q, %s, %q},", t.Token, num(t.Min), t.On)
	}
	w.p("}\n")
	w.p("// AuditBeds are the grounds text is drawn on per region (main, pane, preset); \"a + b\" is b laid over a.")
	w.p("var AuditBeds = map[string][]string{")
	keys := make([]string, 0, len(f.Audit.Beds))
	for k := range f.Audit.Beds {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		w.p("%q: %#v,", k, f.Audit.Beds[k])
	}
	w.p("}\n")
}

func (w *writer) consts(doc string, toks []named, skip func(named) bool) error {
	w.p("// %s", doc)
	w.p("const (")
	for _, t := range toks {
		if skip != nil && skip(t) {
			continue
		}
		v, err := number(t.Value)
		if err != nil {
			return fmt.Errorf("%s: %w", t.Name, err)
		}
		w.comment("\t", ident(t.Name), t.Usage)
		w.p("\t%s = %s", ident(t.Name), num(v))
	}
	w.p(")\n")
	return nil
}

func (w *writer) metrics(f *file) error {
	w.p("// Shadow is a drop shadow at the default text size: Color is a token of the scheme, and a rim of RimWidth in")
	w.p("// RimColor goes around the shape when RimWidth is above 0.")
	w.p("type Shadow struct {\nX, Y, Blur float32\nColor string\nRimWidth float32\nRimColor string\n}\n")
	w.p("var (")
	for _, s := range f.Shadow {
		var v string
		if err := json.Unmarshal(s.Value, &v); err != nil {
			return fmt.Errorf("%s: %w", s.Name, err)
		}
		main, rim, _ := strings.Cut(v, " + ")
		p := strings.Fields(main)
		if len(p) != 4 {
			return fmt.Errorf("%s: want x y blur color, got %q", s.Name, main)
		}
		w.comment("\t", ident(s.Name), s.Usage)
		lit := fmt.Sprintf("Shadow{X: %s, Y: %s, Blur: %s, Color: %q", p[0], p[1], p[2], p[3])
		if rim != "" {
			r := strings.Fields(rim)
			if len(r) != 3 || r[2] != "rim" {
				return fmt.Errorf("%s: want width color rim, got %q", s.Name, rim)
			}
			lit += fmt.Sprintf(", RimWidth: %s, RimColor: %q", r[0], r[1])
		}
		w.p("\t%s = %s}", ident(s.Name), lit)
	}
	w.p(")\n")
	if err := w.consts("Opacities: the only ones a view uses.", f.Opacity.Tokens, nil); err != nil {
		return err
	}
	if err := w.consts("Spacing, in DIPs at the default text size.", f.Space.Tokens, nil); err != nil {
		return err
	}
	pill := func(t named) bool { return string(t.Value) == `"half"` }
	if err := w.consts("Corner radii, in DIPs at the default text size. A pill's radius is half its height.", f.Radius.Tokens, pill); err != nil {
		return err
	}
	if err := w.consts("Sizes, in DIPs at the default text size.", f.Size.Tokens, nil); err != nil {
		return err
	}
	w.p("// SetMetrics sets the metrics of MyGo's theme that tokens stand for, at the default text size.")
	w.p("func SetMetrics(t *ui.Theme) {")
	for _, group := range [][]named{f.Space.Tokens, f.Radius.Tokens, f.Size.Tokens} {
		for _, t := range group {
			if t.MyGo != "" {
				w.p("t.%s = %s", t.MyGo, ident(t.Name))
			}
		}
	}
	w.p("}\n")
	return nil
}

func (w *writer) fonts(f *file) error {
	w.p("// Font families. Sans is empty: MyGo then draws the platform's UI font, or the Interface font chosen in Settings.")
	w.p("const (")
	var serif map[string][]string
	for _, t := range f.Font.Families {
		var s string
		if json.Unmarshal(t.Value, &s) == nil {
			if s == "system" {
				s = ""
			}
			w.p("Font%s = %q", ident(t.Name), s)
			continue
		}
		if err := json.Unmarshal(t.Value, &serif); err != nil {
			return fmt.Errorf("%s: %w", t.Name, err)
		}
	}
	w.p(")\n")
	w.p("// FontSerif is the serif family list per platform (GOOS); the first installed one draws.")
	w.p("var FontSerif = map[string][]string{")
	goos := make([]string, 0, len(serif))
	for k := range serif {
		goos = append(goos, k)
	}
	sort.Strings(goos)
	for _, k := range goos {
		var list []string
		for _, fam := range serif[k] {
			if fam == "system serif" {
				list = append(list, "prefs.SystemSerif")
			} else {
				list = append(list, strconv.Quote(fam))
			}
		}
		w.p("%q: {%s},", k, strings.Join(list, ", "))
	}
	w.p("}\n")
	return nil
}

func (w *writer) text(f *file) error {
	w.p("// Lang is a value in English and in Simplified Chinese, which runs taller.")
	w.p("type Lang struct{ En, Zh float32 }\n")
	w.p("// TextStyle is an interface text style. Size and Line are DIPs at the default text size and Tracking is in em;")
	w.p("// when Relative is set, Size is a multiple of the surrounding text's size and Line a multiple of Size.")
	w.p("type TextStyle struct {\nName string\nSize, Line, Tracking Lang\nWeight int\nMono, Relative bool\n}\n")
	w.p("// The interface text styles, the only ones a screen sets.")
	w.p("var (")
	var all []string
	for _, s := range f.Text.Styles {
		var size, line lang
		if err := json.Unmarshal(s.Size, &size); err != nil {
			return fmt.Errorf("%s size: %w", s.Name, err)
		}
		if err := json.Unmarshal(s.Line, &line); err != nil {
			return fmt.Errorf("%s line: %w", s.Name, err)
		}
		relative := strings.HasSuffix(string(s.Size), `em"`)
		var track lang
		if s.Tracking != nil {
			track = *s.Tracking
		}
		name := "Text" + ident(s.Name)
		all = append(all, name)
		w.comment("\t", name, s.Usage)
		w.p("\t%s = TextStyle{Name: %q, Size: Lang{%s, %s}, Line: Lang{%s, %s}, Tracking: Lang{%s, %s}, Weight: %d, Mono: %t, Relative: %t}",
			name, s.Name, num(size.En), num(size.Zh), num(line.En), num(line.Zh), num(track.En), num(track.Zh), s.Weight, s.Mono, relative)
	}
	w.p(")\n")
	w.p("// TextStyles lists every interface text style.")
	w.p("var TextStyles = []TextStyle{%s}\n", strings.Join(all, ", "))
	return nil
}

func (w *writer) reading(f *file) {
	r := f.Reading
	w.p("// ReadingStyle is a style of the article: Size in DIPs at the default reading size, Line a multiple of the size,")
	w.p("// Tracking in em.")
	w.p("type ReadingStyle struct {\nSize float32\nLine, Tracking Lang\nWeight int\nMono bool\n}\n")
	w.p("// The reading scale.")
	w.p("var (")
	for _, s := range []struct {
		name string
		v    struct {
			Size     float64
			Line     lang
			Weight   int
			Tracking *lang
			Mono     bool
		}
	}{{"ReadingBody", r.Body}, {"ReadingH2", r.H2}, {"ReadingH3", r.H3}, {"ReadingCode", r.Code}} {
		var t lang
		if s.v.Tracking != nil {
			t = *s.v.Tracking
		}
		weight := s.v.Weight
		if weight == 0 {
			weight = 400
		}
		w.p("%s = ReadingStyle{Size: %s, Line: Lang{%s, %s}, Tracking: Lang{%s, %s}, Weight: %d, Mono: %t}",
			s.name, num(s.v.Size), num(s.v.Line.En), num(s.v.Line.Zh), num(t.En), num(t.Zh), weight, s.v.Mono)
	}
	w.p(")\n")
	w.p("// The reading choices: text size, line spacing and line width, by the name Settings stores.")
	w.p("var (")
	w.p("ReadingSizeScale = %s", floatMap(r.SizeScale))
	w.p("ReadingLineWidth = %s", floatMap(r.LineWidth))
	keys := make([]string, 0, len(r.LineSpacing))
	for k := range r.LineSpacing {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		v := r.LineSpacing[k]
		parts = append(parts, fmt.Sprintf("%q: {%s, %s}", k, num(v.En), num(v.Zh)))
	}
	w.p("ReadingLineSpacing = map[string]Lang{%s}", strings.Join(parts, ", "))
	w.p(")\n")
	w.p("// Spaces of the article, in em of the body size.")
	w.p("const (")
	w.p("ReadingParagraph = %s", num(r.Paragraph.En))
	w.p("ReadingListItem = %s", num(r.ListItem.En))
	w.p("ReadingH2Above = %s", num(r.H2Space.Above.En))
	w.p("ReadingH2Below = %s", num(r.H2Space.Below.En))
	w.p("// HanLatin is the space between Han and Latin text; PunctuationCompression what a pair of full-width marks")
	w.p("// gives back.")
	w.p("HanLatin = %s", num(r.HanLatin.En))
	w.p("PunctuationCompression = %s", num(r.PunctuationCompression.En))
	w.p(")\n")
}

func floatMap(m map[string]float64) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%q: %s", k, num(m[k])))
	}
	return "map[string]float32{" + strings.Join(parts, ", ") + "}"
}

func (w *writer) motion(f *file) error {
	w.p("// Motion: durations of the fixed curves named beside them, and the scale a popup opens from.")
	w.p("const (")
	for _, t := range f.Motion.Tokens {
		w.comment("\t", ident(t.Name), t.Usage)
		if t.Duration > 0 {
			w.p("\t%s = %s * time.Millisecond // %s", ident(t.Name), num(t.Duration), t.Curve)
			continue
		}
		v, err := number(t.Value)
		if err != nil {
			return fmt.Errorf("%s: %w", t.Name, err)
		}
		w.p("\t%s = %s", ident(t.Name), num(v))
	}
	w.p(")")
	return nil
}
