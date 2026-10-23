package reader

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/ui/icons"
	"github.com/yangflow/oriel/internal/ui/theme"
)

const (
	maxSelection = 2000 // the longest text the server translates or explains, in characters
	quoteLength  = 40   // the characters of the text asked about that the card's head shows
)

// selectionMenu adds to the menu of the body what the selected text can be asked: its translation or an explanation,
// then Copy and Select All. The items show with a selection only; the menu is built again in the frame after an item
// is chosen, with the selection as it was.
func (s *Screen) selectionMenu(m *ui.Menu, body ui.Element) {
	text := selectedText(body)
	if text != "" {
		for _, task := range []struct{ name, label string }{
			{"translate", s.text.TranslateSelection}, {"explain", s.text.ExplainSelection},
		} {
			if m.Item(task.label).Chosen() {
				s.ask(task.name, text)
			}
		}
		m.Separator()
	}
	m.EditItems()
}

// selectedText is the text selected in e, its paragraphs one to a line.
func selectedText(e ui.Element) string {
	var parts []string
	for _, sel := range e.SelectedTexts() {
		runes := []rune(sel.Text)
		start, end := min(max(sel.Start, 0), len(runes)), min(max(sel.End, 0), len(runes))
		if start < end {
			parts = append(parts, string(runes[start:end]))
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

// ask shows the card for task on text and asks the server, unless the text is too long, which the card says without
// a request.
func (s *Screen) ask(task, text string) {
	if s.api == nil {
		return
	}
	sel := &s.sel
	*sel = selection{on: true, task: task, text: text, ticket: sel.ticket + 1}
	if utf8.RuneCountInString(text) > maxSelection {
		sel.long = true
		s.announce = s.text.SelectLess
		return
	}
	sel.working = true
	gen, ticket, api, title := s.gen, sel.ticket, s.api, s.article.Entry.Title
	s.deps.Go(func() func() {
		answer, model, err := api.Selection(context.Background(), task, text, title)
		return func() {
			if gen != s.gen || ticket != s.sel.ticket {
				return
			}
			sel := &s.sel
			sel.working = false
			switch {
			case s.deps.Expired(err):
				s.closeSelection()
			case errorCode(err) == "ai_not_configured":
				sel.aiOff = true
				s.announce = s.text.AIOffTitle
			case err != nil:
				sel.failed, sel.code = true, errorCode(err)
				s.announce = s.reason(sel.code)
			default:
				sel.answer, sel.model = strings.TrimSpace(answer), model
				s.announce = s.text.TranslationReady
				if task == "explain" {
					s.announce = s.text.ExplanationReady
				}
			}
		}
	})
}

// closeSelection closes the card, and drops the answer still to come.
func (s *Screen) closeSelection() {
	s.sel = selection{ticket: s.sel.ticket + 1}
}

// selectionCard is the card over the foot of the reader: the task and the start of the text asked about, then the
// answer, the lines of one coming, why there is none, or the word that the text is too long.
func (s *Screen) selectionCard(c *ui.Context) {
	sel := &s.sel
	if !sel.on {
		return
	}
	t := c.Theme()
	_, height := c.Size()
	name, icon, color, background := s.text.TranslateSelection, icons.Languages, t.Accent, theme.RaisedFill(t)
	if sel.task == "explain" {
		name, icon = s.text.ExplainSelection, icons.Sparkles
	}
	switch {
	case sel.failed:
		color, background = t.Danger, background.Mix(t.Danger, 0.09)
		icon = icons.TriangleAlert
	case sel.aiOff:
		name, icon, color = s.text.AIOffTitle, icons.Sparkles, theme.Text2(t)
	case sel.long:
		color = theme.Text2(t)
	}
	ui.Row(c).Absolute().Left(0).Right(0).Bottom(theme.Px(t, 16)).Padding(0, theme.Px(t, 16)).Justify(ui.Center).PassThrough().
		Children(func() {
			ui.Column(c).FillWidth().MaxWidth(s.column(t)).Padding(theme.Px(t, 12), theme.Px(t, 16), theme.Px(t, 14), theme.Px(t, 16)).
				Radius(theme.Px(t, 14)).Background(background).Border(0.5, t.Border).
				Shadow(0, 6, 24, 0, ui.RGBA(0, 0, 0, 0.16)).Role(ui.RoleGroup).Label(name).Children(func() {
				s.head(c, icon, color, name, func() {
					if !sel.aiOff {
						ui.Text(c, "“"+quote(sel.text)+"”").FontSize(theme.Px(t, 12)).TextColor(t.TextMuted).SingleLine().
							MinWidth(0).Shrink(1)
					}
					if s.mini(c, s.text.Close) {
						s.closeSelection()
					}
				})
				switch {
				case sel.working:
					for _, width := range []float32{96, 80, 52} {
						ui.Row(c).FillWidth().Margin(theme.Px(t, 9), 0, 0, 0).Children(func() {
							ui.Box(c).Grow(width).Basis(0).Height(theme.Px(t, 10)).Radius(theme.Pill(theme.Px(t, 10))).Background(theme.Ink(t, 0.08))
							ui.Box(c).Grow(100 - width).Basis(0)
						})
					}
				case sel.failed:
					reason := strings.ReplaceAll(s.text.SelectionFailed, "{reason}", s.reason(sel.code))
					s.cardText(c, reason, t.Text).Role(ui.RoleStatus)
					ui.Row(c).Children(func() {
						if s.small(c, icons.RefreshCw, s.text.Retry, false) {
							s.ask(sel.task, sel.text)
						}
					})
				case sel.aiOff:
					s.cardText(c, s.text.SelectionAIOff, theme.Text2(t)).Role(ui.RoleStatus)
					ui.Row(c).Children(func() {
						if s.small(c, icons.Settings, s.text.OpenSettings, true) && s.deps.OpenSettings != nil {
							s.deps.OpenSettings()
						}
					})
				case sel.long:
					ui.Column(c).Role(ui.RoleStatus).Children(func() {
						s.cardText(c, s.text.SelectLess, t.Text).FontWeight(600)
						s.cardText(c, s.text.SelectLessHint, theme.Text2(t))
					})
				default:
					ui.Scroll(c).FillWidth().MaxHeight(float32(height) * 0.4).Children(func() {
						ui.Column(c).FillWidth().Selectable().Children(func() { s.cardText(c, sel.answer, t.Text) })
					})
					ui.Row(c).AlignItems(ui.Center).Gap(t.Space(2)).Children(func() {
						if s.small(c, nil, s.text.CopyAnswer, false) {
							c.WriteClipboard(sel.answer)
							s.announce = s.text.Copied
						}
						if sel.model != "" {
							by := strings.ReplaceAll(s.text.By, "{model}", sel.model)
							ui.Text(c, by).Margin(theme.Px(t, 10), 0, 0, 0).FontSize(theme.Px(t, 12)).TextColor(t.TextMuted).SingleLine().
								MinWidth(0).Shrink(1)
						}
					})
				}
			})
		})
}

// cardText is a line of text of a card, 13 high, under its head.
func (s *Screen) cardText(c *ui.Context, text string, color ui.Color) ui.Element {
	t := c.Theme()
	return ui.Text(c, text).Margin(theme.Px(t, 6), 0, 0, 0).FontSize(theme.Px(t, 13)).LineHeight(cardLine(text)).TextColor(color).Wrap()
}

// quote is the start of text for the head of the card: its first characters, on one line.
func quote(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(text) <= quoteLength {
		return text
	}
	return string([]rune(text)[:quoteLength]) + "…"
}
