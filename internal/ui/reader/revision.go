package reader

import (
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/ui/icons"
	"github.com/yangflow/oriel/internal/ui/locale"
	"github.com/yangflow/oriel/internal/ui/theme"
)

// What changed when an article was updated, see docs/product.md "条目的修订" and "阅读器": a notice under the title says
// when, and opens a paragraph-by-paragraph difference between the version before and the one now.
const (
	kept    = iota // paragraphs both versions have, folded into one line with their count
	removed        // a paragraph only the earlier version has
	added          // a paragraph only the version now has
)

// change is a line of the difference: a paragraph that was removed or added, the title when title is set, or n
// paragraphs both versions have.
type change struct {
	kind  int
	text  string
	n     int
	title bool
}

// revision is the notice of an updated article: whether the earlier version has been got, and the difference.
type revision struct {
	ready   bool
	changes []change
	open    bool
}

// diffParagraphs is the difference from before to now by their longest common run of paragraphs, with each run of
// paragraphs both have folded into one kept line. A changed title comes first.
func diffParagraphs(before, now client.RevisionVersion) []change {
	var out []change
	if a, b := strings.Join(strings.Fields(before.Title), " "), strings.Join(strings.Fields(now.Title), " "); a != b {
		out = append(out, change{kind: removed, text: a, title: true}, change{kind: added, text: b, title: true})
	}
	x, y := before.Paragraphs, now.Paragraphs
	// lcs[i][j] is the length of the longest common run of x[i:] and y[j:].
	lcs := make([][]int, len(x)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(y)+1)
	}
	for i := len(x) - 1; i >= 0; i-- {
		for j := len(y) - 1; j >= 0; j-- {
			switch {
			case x[i] == y[j]:
				lcs[i][j] = lcs[i+1][j+1] + 1
			case lcs[i+1][j] >= lcs[i][j+1]:
				lcs[i][j] = lcs[i+1][j]
			default:
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	keep := func() {
		if n := len(out) - 1; n >= 0 && out[n].kind == kept {
			out[n].n++
		} else {
			out = append(out, change{kind: kept, n: 1})
		}
	}
	i, j := 0, 0
	for i < len(x) || j < len(y) {
		switch {
		case i < len(x) && j < len(y) && x[i] == y[j]:
			keep()
			i, j = i+1, j+1
		case i < len(x) && (j == len(y) || lcs[i+1][j] >= lcs[i][j+1]):
			out = append(out, change{kind: removed, text: x[i]})
			i++
		default:
			out = append(out, change{kind: added, text: y[j]})
			j++
		}
	}
	return out
}

// loadRevision gets the earlier version of the open article when it was updated, and works the difference out away
// from the UI thread. A failure leaves the notice without the button to see the changes.
func (s *Screen) loadRevision(e client.Entry) {
	s.rev = revision{}
	if s.api == nil || e.RevisedAt == 0 {
		return
	}
	gen, api, id, ctx := s.gen, s.api, e.ID, s.current.get()
	s.deps.Go(func() func() {
		r, err := api.Revision(ctx, id)
		var changes []change
		if err == nil {
			changes = diffParagraphs(r.Previous, r.Current)
		}
		return func() {
			if gen != s.gen {
				return
			}
			if s.deps.Expired(err) || err != nil {
				return
			}
			s.rev.ready, s.rev.changes = true, changes
		}
	})
}

// revisedLine is the notice under the title of an article that was updated: when, and the button that shows or hides
// what changed.
func (s *Screen) revisedLine(c *ui.Context, e client.Entry) {
	if e.RevisedAt == 0 {
		return
	}
	t := c.Theme()
	when := locale.DateTime(s.lang, s.hours, s.deps.Now(), time.UnixMilli(e.RevisedAt))
	text := strings.ReplaceAll(s.text.Revised, "{time}", when)
	ui.Column(c).Key("revised").FillWidth().Margin(theme.Px(t, 14), 0, 0, 0).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Padding(theme.Px(t, 6), theme.Px(t, 6), theme.Px(t, 6), theme.Px(t, 14)).
			Radius(theme.Px(t, 12)).Background(t.Surface).Role(ui.RoleStatus).Label(text).Children(func() {
			ui.Icon(c, icons.Pencil).FontSize(theme.IconSmall(t)).TextColor(t.TextMuted)
			ui.Text(c, text).Grow(1).MinWidth(0).FontSize(theme.Px(t, 12)).TextColor(theme.Text2(t)).Wrap()
			if !s.rev.ready || len(s.rev.changes) == 0 {
				return
			}
			label, icon := s.text.ShowChanges, icons.ChevronDown
			if s.rev.open {
				label, icon = s.text.HideChanges, icons.ChevronUp
			}
			b := ui.ButtonBase(c).Height(theme.Px(t, 26)).PaddingX(theme.Px(t, 8)).Gap(theme.Px(t, 4)).Radius(theme.Px(t, 8)).
				Label(label).Role(ui.RoleDisclosure).Expanded(s.rev.open)
			color := theme.Text2(t)
			switch {
			case b.Pressed():
				b.Background(t.SurfacePressed)
				color = t.Text
			case b.Hovered():
				b.Background(t.SurfaceHover)
				color = t.Text
			}
			b.Children(func() {
				ui.Text(c, label).FontSize(theme.Px(t, 12)).FontWeight(500).NoWrap().TextColor(color)
				ui.Icon(c, icon).FontSize(theme.IconSmall(t)).TextColor(color)
			})
			if b.Clicked() {
				s.rev.open = !s.rev.open
			}
		})
		if s.rev.open && s.rev.ready {
			s.changeList(c)
		}
	})
}

// changeList is the difference: a removed or added paragraph each in a tinted box that also says which it is in
// words and with − or +, so that color is not all that tells them apart.
func (s *Screen) changeList(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).FillWidth().Gap(theme.Px(t, 6)).Margin(theme.Px(t, 8), 0, 0, 0).Role(ui.RoleList).Label(s.text.Changes).Children(func() {
		for i, ch := range s.rev.changes {
			if ch.kind == kept {
				label := strings.ReplaceAll(s.text.Unchanged, "{n}", strconv.Itoa(ch.n))
				ui.Text(c, label).Key(i).FontSize(theme.Px(t, 12)).TextColor(t.TextMuted).Margin(theme.Px(t, 2), 0, theme.Px(t, 2), theme.Px(t, 14)).
					Role(ui.RoleListItem)
				continue
			}
			word, sign, color := s.text.Removed, "−", t.Danger
			if ch.kind == added {
				word, sign, color = s.text.Added, "+", t.Accent
			}
			if ch.title {
				word += " · " + s.text.TitleLabel
			}
			ui.Row(c).Key(i).FillWidth().AlignItems(ui.Start).Gap(t.Space(2)).Padding(theme.Px(t, 8), theme.Px(t, 12)).Radius(theme.Px(t, 10)).
				Background(theme.Soft(t, color)).Role(ui.RoleListItem).Label(word + ": " + ch.text).Children(func() {
				ui.Text(c, sign).FontSize(theme.Px(t, 13)).FontWeight(600).TextColor(color).NoWrap().Width(theme.Px(t, 12))
				ui.Column(c).Grow(1).MinWidth(0).Gap(theme.Px(t, 2)).Children(func() {
					ui.Text(c, word).FontSize(theme.Px(t, 11)).FontWeight(600).TextColor(color).NoWrap()
					ui.Text(c, ch.text).FontSize(theme.Px(t, 13)).TextColor(t.Text).Wrap()
				})
			})
		}
	})
}
