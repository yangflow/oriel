package timeline

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/ui/icons"
	"github.com/yangflow/oriel/internal/ui/theme"
)

// pack is a starter pack: a topic and the public feeds that start it. The name and the one-line description are in
// copy.json under the pack's ID; the titles of the feeds come from the feeds. Nothing here is contacted until a
// pack is chosen (docs/product.md, Timeline, starter packs).
type pack struct {
	id    string
	feeds []string
}

// packs are kept small: well-known sites with public, free and long-lived feeds, at most six per pack, each checked
// by hand to answer with a feed before it went in.
var packs = []pack{
	{"technology", []string{
		"https://news.ycombinator.com/rss",
		"https://www.theverge.com/rss/index.xml",
		"http://feeds.arstechnica.com/arstechnica/index",
		"https://www.wired.com/feed/rss",
		"https://daringfireball.net/feeds/main",
	}},
	{"science", []string{
		"https://www.nature.com/nature.rss",
		"https://www.quantamagazine.org/feed/",
		"https://www.sciencedaily.com/rss/all.xml",
		"https://www.nasa.gov/feed/",
	}},
	{"design", []string{
		"https://www.smashingmagazine.com/feed/",
		"https://alistapart.com/main/feed/",
		"https://css-tricks.com/feed/",
		"https://www.designboom.com/feed/",
	}},
	{"news", []string{
		"https://feeds.bbci.co.uk/news/world/rss.xml",
		"https://www.theguardian.com/world/rss",
		"https://feeds.npr.org/1001/rss.xml",
		"https://www.aljazeera.com/xml/rss/all.xml",
	}},
}

// packText is the name and description of a pack.
type packText struct {
	Name, Hint string
}

// packResult is what choosing a pack did: the subscriptions added, how many were followed already, and the sites
// whose feeds could not be added.
type packResult struct {
	added   []client.Source
	skipped int
	failed  []string
}

// host is the site of a feed address, without a leading www.
func host(address string) string {
	u, err := url.Parse(address)
	if err != nil || u.Hostname() == "" {
		return address
	}
	return strings.TrimPrefix(u.Hostname(), "www.")
}

// subscribePack adds the feeds of p, from the empty first-run screen. A feed that cannot be added does not stop the
// others; one already followed is skipped. The result is a toast, and the timeline reads the sources again when any
// was added. Choosing while a pack is being added does nothing.
func (s *Screen) subscribePack(p pack) {
	if s.api == nil || s.packing != "" {
		return
	}
	s.packing = p.id
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		var res packResult
		var err error
		for _, address := range p.feeds {
			src, e := api.AddSource(context.Background(), client.NewSource{URL: address, Kind: "article"})
			var apiErr *client.APIError
			switch {
			case e == nil:
				res.added = append(res.added, src)
			case errors.As(e, &apiErr) && apiErr.Code == "source_exists":
				res.skipped++
			case errors.Is(e, client.ErrUnauthorized):
				err = e
			default:
				res.failed = append(res.failed, host(address))
			}
			if err != nil {
				break
			}
		}
		return func() {
			if gen != s.gen {
				return
			}
			s.packing = ""
			if s.expired(err) {
				return
			}
			s.packDone(p, res)
		}
	})
}

// packDone shows the result of a pack and the subscriptions it added.
func (s *Screen) packDone(p pack, res packResult) {
	n := len(res.added)
	var text string
	switch {
	case n == 0 && len(res.failed) > 0:
		text = s.text.PackNone
	case len(res.failed) > 0:
		text = s.text.PackSome
	case n == 1:
		text = s.text.PackOne
	default:
		text = s.text.PackAdded
	}
	text = strings.NewReplacer("{n}", strconv.Itoa(n), "{pack}", s.text.Packs[p.id].Name,
		"{names}", strings.Join(res.failed, ", ")).Replace(text)
	s.result = &refreshed{text: text, alert: len(res.failed) > 0}
	if n > 0 {
		s.sources = append(s.sources, res.added...)
		s.Refresh()
	}
}

// packCards is the label and the row of starter packs under the buttons of the first-run screen.
func (s *Screen) packCards(c *ui.Context) {
	t := c.Theme()
	adding := s.packing != ""
	label := s.text.PacksLabel
	for _, p := range packs {
		if adding && p.id == s.packing {
			label = s.format(s.text.PacksAdding, len(p.feeds))
		}
	}
	row := ui.Row(c).Gap(px(t, 6)).AlignItems(ui.Center).Margin(t.Space(3), 0, 0, 0)
	if adding {
		row.Role(ui.RoleStatus)
	}
	row.Children(func() {
		if adding {
			spin := ui.Icon(c, icons.LoaderCircle).FontSize(px(t, 14)).TextColor(t.TextMuted)
			spin.Rotate(spin.Loop("spin", 900*time.Millisecond, ui.Linear) * 360)
		}
		ui.Text(c, label).FontSize(px(t, 12)).TextColor(t.TextMuted)
	})
	ui.Row(c).Wrap().Justify(ui.Center).Gap(t.Space(2)).MaxWidth(px(t, 560)).Label(s.text.PacksLabel).
		Role(ui.RoleGroup).Children(func() {
		for _, p := range packs {
			text := s.text.Packs[p.id]
			b := ui.ButtonBase(c).Key(p.id).Width(px(t, 132)).Column().AlignItems(ui.Start).Gap(px(t, 2)).
				Padding(px(t, 10), px(t, 12)).Radius(px(t, 12)).Background(theme.Track(t)).
				Label(text.Name + ". " + text.Hint).Disabled(adding)
			if adding {
				b.Opacity(0.5)
			} else if b.Pressed() {
				b.Background(theme.Track(t).Mix(theme.Ink(t, 1), 0.16))
			} else if b.Hovered() {
				b.Background(theme.Track(t).Mix(theme.Ink(t, 1), 0.1))
			}
			theme.Fade(b)
			b.Children(func() {
				ui.Text(c, text.Name).FontWeight(600).TextColor(t.Text)
				ui.Text(c, text.Hint).FontSize(px(t, 12)).TextColor(t.TextMuted).Wrap()
			})
			if b.Clicked() {
				s.subscribePack(p)
			}
		}
	})
}
