package tracker

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestCounts(t *testing.T) {
	for markup, want := range map[string]bool{
		`<img src="https://a.example/p.png">`:                      false,
		`<img src="https://a.example/p.png" width="1" height="1">`: true,
		`<img src="https://a.example/p.png" height="2">`:           true,
		`<img src="https://a.example/p.png" width="640">`:          false,
		`<img src="https://a.example/p.png" hidden>`:               true,
		`<img src="https://a.example/p.png" style="display:none">`: true,
		`<img src="https://a.example/p.png" style="width: 1px">`:   true,
		`<img src="https://a.example/p.png" style="width: 300px">`: false,
		`<img src="https://pixel.wp.com/g.gif?v=ext">`:             true,
	} {
		doc, err := html.Parse(strings.NewReader(markup))
		if err != nil {
			t.Fatal(err)
		}
		var img *html.Node
		var find func(*html.Node)
		find = func(n *html.Node) {
			if n.Type == html.ElementNode && n.Data == "img" {
				img = n
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				find(c)
			}
		}
		find(doc)
		if got := Counts(img, attr(img, "src")); got != want {
			t.Errorf("%s: %v", markup, got)
		}
	}
}
