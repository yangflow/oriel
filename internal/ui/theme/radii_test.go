package theme

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestRadiiAreTiers keeps corner radii to the tiers of design/tokens.md: a radius outside them in any screen package
// fails here until it is merged into a tier or listed there.
//
// Every Radius(...) argument and every `Radius:` field is read. A literal (a bare number, Px(t, n), t.Space(k) as
// 4k, Rem(n / 13)) must be a tier; Pill(...) is the one radius that follows its shape (a pill or a circle) and
// t.Radius is the theme's own 8. A non-literal argument must be one of the forms listed below, whose values are
// checked at the places that produce them: the callers of picture and coverOf, GlyphRadius and SheetLook.
func TestRadiiAreTiers(t *testing.T) {
	allowed := map[float64]bool{
		4: true, 6: true, 8: true, 10: true, 12: true, 14: true, 16: true, // the tiers
		2: true, // the selection marker, an exception in design/tokens.md
		0: true, // square corners
	}
	forms := map[string]bool{
		"t.Radius":          true,
		"th.Radius":         true,
		"radius":            true, // a parameter: picture and coverOf callers and GlyphRadius, checked below
		"h.Radius":          true, // theme.Hit: its callers are read as `Radius:` fields
		"Px(t, l.Radius)":   true, // SheetLook, whose literals are read as `Radius:` and in the assignment below
		"l.Radius":          true,
		"GlyphRadius(size)": true,
	}
	number := regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)
	px := regexp.MustCompile(`^(?:theme\.)?Px\([A-Za-z.]+, ?([0-9.]+)\)$`)
	space := regexp.MustCompile(`^[A-Za-z.]+\.Space\(([0-9.]+)\)$`)
	rem := regexp.MustCompile(`^[A-Za-z.]+\.Rem\(([0-9.]+) ?/ ?13\)$`)
	pill := regexp.MustCompile(`^(?:theme\.)?Pill\(.+\)$`)
	// Literals that reach a radius through a parameter or an assignment.
	via := []*regexp.Regexp{
		regexp.MustCompile(`\b(?:picture|pictureOf)\(c, [^,]+, [^,]+, (?:theme\.Px\(t, )?([0-9.]+)\)?,`),
		regexp.MustCompile(`\bcoverOf\(c, e, [^,]+, [^,]+, (?:theme\.Px\(t, )?([0-9.]+)\)?,`),
		regexp.MustCompile(`\bl\.Radius, l\.Offset, l\.Blur, l\.Shadow = ([0-9.]+),`),
		regexp.MustCompile(`\bRadius: ([0-9.]+),`),
	}
	check := func(where string, v float64, text string) {
		if !allowed[v] {
			t.Errorf("%s: radius %s is not a tier or an exception in design/tokens.md", where, text)
		}
	}
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(src), "\n") {
			where := path + ":" + strconv.Itoa(i+1)
			for _, re := range via {
				for _, m := range re.FindAllStringSubmatch(line, -1) {
					if v, err := strconv.ParseFloat(m[1], 64); err == nil {
						check(where, v, m[1])
					}
				}
			}
			for at := 0; ; {
				j := strings.Index(line[at:], ".Radius(")
				if j < 0 {
					break
				}
				start := at + j + len(".Radius(")
				depth, end := 1, start
				for ; end < len(line) && depth > 0; end++ {
					switch line[end] {
					case '(':
						depth++
					case ')':
						depth--
					}
				}
				arg := strings.TrimSpace(line[start : end-1])
				at = end
				switch {
				case number.MatchString(arg):
					v, _ := strconv.ParseFloat(arg, 64)
					check(where, v, arg)
				case px.MatchString(arg):
					v, _ := strconv.ParseFloat(px.FindStringSubmatch(arg)[1], 64)
					check(where, v, arg)
				case space.MatchString(arg):
					v, _ := strconv.ParseFloat(space.FindStringSubmatch(arg)[1], 64)
					check(where, v*4, arg)
				case rem.MatchString(arg):
					v, _ := strconv.ParseFloat(rem.FindStringSubmatch(arg)[1], 64)
					check(where, v, arg)
				case pill.MatchString(arg), forms[arg]:
				default:
					t.Errorf("%s: Radius(%s) is neither a literal nor a listed form; add the form here with its literals checked", where, arg)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
