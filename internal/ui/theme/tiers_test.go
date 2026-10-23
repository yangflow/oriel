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

// TestTextSizesAreTiers keeps text sizes to the five of design/tokens.md and its exceptions: a text size outside
// them in any screen package fails here until the size is merged into a tier or listed there.
//
// Every FontSize argument that is not on an icon is read. A literal (a bare number, Px(t, n), Rem(x) as x * 13)
// must be a tier or an exception. A non-literal argument must be one of the forms listed below, each of which has
// its literals checked separately.
func TestTextSizesAreTiers(t *testing.T) {
	allowed := map[float64]bool{
		24: true, 15: true, 13: true, 12: true, 11: true, // the five
		21: true, 10.5: true, 9: true, // exceptions in design/tokens.md
	}
	// Non-literal forms: the reader title (its size is checked as `size := Px(t, n)` below), the reading scale
	// (the sixth scale of tokens.md, not interface type), the monogram letter and the title text of an entry (their
	// literals are checked below).
	forms := map[string]bool{
		"size":                             true,
		"Px(t, font)":                      true,
		"theme.Px(t, size)":                true,
		"theme.Px(t, 13)*scale":            true,
		"theme.Px(t, 16) * scale":          true,
		"theme.Px(t, 16*0.86) * s.scale()": true,
	}
	number := regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?$`)
	px := regexp.MustCompile(`^(?:theme\.)?Px\([A-Za-z.()]+, ?([0-9.]+)\)$`)
	rem := regexp.MustCompile(`^[A-Za-z.()]+\.Rem\(([0-9.]+)\)$`)
	// Literals that reach FontSize through a variable or a parameter.
	via := []*regexp.Regexp{
		regexp.MustCompile(`titleText\(c, e, ([0-9.]+),`),
		regexp.MustCompile(`\bsize :?= (?:theme\.)?Px\(t, ([0-9.]+)\)`),
		regexp.MustCompile(`\bfont(?:, [a-z]+)? :?= (?:float32\()?([0-9.]+)\)?`),
		regexp.MustCompile(`\bfont = ([0-9.]+)$`),
	}
	check := func(where string, v float64, text string) {
		if !allowed[v] {
			t.Errorf("%s: text size %s is not one of the five tiers or an exception in design/tokens.md", where, text)
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
			if strings.Contains(line, "ui.Icon(") {
				continue // icons are not type
			}
			for at := 0; ; {
				j := strings.Index(line[at:], "FontSize(")
				if j < 0 {
					break
				}
				start := at + j + len("FontSize(")
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
				case rem.MatchString(arg):
					v, _ := strconv.ParseFloat(rem.FindStringSubmatch(arg)[1], 64)
					check(where, v*13, arg)
				case forms[arg]:
				default:
					t.Errorf("%s: FontSize(%s) is neither a literal nor a listed form; add the form here with its literals checked", where, arg)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
