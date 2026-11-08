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

// guarded lists the packages whose views take every size, color and motion from the tokens.
var guarded = []string{"timeline", "reader", "settings", "subscriptions", "connect", "theme"}

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
	// A number added to or taken from a size is one too; halving or doubling a token is not.
	if b, ok := expr.(*ast.BinaryExpr); ok && (b.Op == token.ADD || b.Op == token.SUB) {
		return bare(b.X) || bare(b.Y)
	}
	if p, ok := expr.(*ast.ParenExpr); ok {
		return bare(p.X)
	}
	lit, ok := expr.(*ast.BasicLit)
	return ok && (lit.Kind == token.INT || lit.Kind == token.FLOAT) && strings.Trim(lit.Value, "0.") != ""
}

// alphaArg is the argument of a call that says an opacity or a mix amount, or nil.
func alphaArg(name string, call *ast.CallExpr) ast.Expr {
	switch {
	case (name == "Alpha" || name == "Opacity") && len(call.Args) == 1:
		return call.Args[0]
	case (name == "Mix" || name == "Ink") && len(call.Args) == 2:
		return call.Args[1]
	}
	return nil
}

// one reports whether expr is the number 1: all of a color, not an opacity.
func one(expr ast.Expr) bool {
	lit, ok := expr.(*ast.BasicLit)
	return ok && lit.Value == "1"
}

// literalTime reports whether expr is a duration written in place, such as 140 * time.Millisecond.
func literalTime(expr ast.Expr) bool {
	b, ok := expr.(*ast.BinaryExpr)
	if !ok || b.Op != token.MUL {
		return false
	}
	_, x := b.X.(*ast.BasicLit)
	_, y := b.Y.(*ast.BasicLit)
	return x || y
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
				if kv, ok := n.(*ast.KeyValueExpr); ok {
					if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Duration" && literalTime(kv.Value) {
						t.Errorf("%s: a duration written in place; use a motion token", fset.Position(kv.Pos()))
					}
					return true
				}
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "Px" {
					for _, arg := range call.Args {
						if bare(arg) {
							t.Errorf("%s: Px with a bare number; use a token or a named constant", fset.Position(call.Pos()))
						}
					}
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if amount := alphaArg(sel.Sel.Name, call); amount != nil && bare(amount) && !one(amount) {
					t.Errorf("%s: an opacity written in place; use a token", fset.Position(call.Pos()))
				}
				if sel.Sel.Name == "Loop" && len(call.Args) > 1 && literalTime(call.Args[1]) {
					t.Errorf("%s: a duration written in place; use a motion token", fset.Position(call.Pos()))
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
