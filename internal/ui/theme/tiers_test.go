package theme

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestTextSizesAreTiers keeps text sizes to the five of design/tokens.md and its exceptions: a literal text size
// outside them in any screen package fails here until the size is merged into a tier or listed there.
func TestTextSizesAreTiers(t *testing.T) {
	allowed := map[string]bool{
		"24": true, "15": true, "13": true, "12": true, "11": true, // the five
		"21": true, "10.5": true, "9": true, // exceptions in design/tokens.md
	}
	size := regexp.MustCompile(`FontSize\((?:theme\.)?Px\((?:t|th), ?([0-9.]+)\)\)`)
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(src), "\n") {
			if strings.Contains(line, "ui.Icon(") {
				continue // icons are not type
			}
			for _, m := range size.FindAllStringSubmatch(line, -1) {
				if !allowed[m[1]] {
					t.Errorf("%s:%d: text size %s is not one of the five tiers or an exception in design/tokens.md", path, i+1, m[1])
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
