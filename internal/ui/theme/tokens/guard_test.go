package tokens_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// guarded lists the screens whose views take every size and color from the tokens; a new screen joins when it is clean.
var guarded = []string{"timeline"}

// sized are the element methods whose arguments are sizes, and px the helper that scales one.
var sized = map[string]bool{
	"Radius": true, "Gap": true, "Size": true, "Width": true, "Height": true, "MinWidth": true, "MaxWidth": true,
	"MinHeight": true, "MaxHeight": true, "Padding": true, "PaddingX": true, "PaddingY": true, "Margin": true,
	"Left": true, "Right": true, "Top": true, "Bottom": true, "FontSize": true, "Px": true,
}

// bare reports whether expr is a number written in place, other than zero.
func bare(expr ast.Expr) bool {
	if u, ok := expr.(*ast.UnaryExpr); ok {
		expr = u.X
	}
	lit, ok := expr.(*ast.BasicLit)
	return ok && (lit.Kind == token.INT || lit.Kind == token.FLOAT) && strings.Trim(lit.Value, "0.") != ""
}

// TestViewsUseTokens fails on a number written as a size, or a color written in place, in a guarded screen.
func TestViewsUseTokens(t *testing.T) {
	for _, screen := range guarded {
		dir := filepath.Join("..", "..", screen)
		names, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil || len(names) == 0 {
			t.Fatalf("%s: no sources: %v", dir, err)
		}
		for _, name := range names {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			src, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, name, src, 0)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if x, ok := sel.X.(*ast.Ident); ok && x.Name == "ui" && (sel.Sel.Name == "Hex" || sel.Sel.Name == "RGBA") {
					t.Errorf("%s: a color written in place; use a token", fset.Position(call.Pos()))
				}
				if sized[sel.Sel.Name] {
					for _, arg := range call.Args {
						if bare(arg) {
							t.Errorf("%s: %s with a bare number; use a token or a named constant", fset.Position(call.Pos()), sel.Sel.Name)
						}
					}
				}
				return true
			})
		}
	}
}
