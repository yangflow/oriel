package reader

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/ui/icons"
	"github.com/yangflow/oriel/internal/ui/theme"
	"github.com/yangflow/oriel/internal/ui/theme/tokens"
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
	s.announce = s.working(task)
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

// working is what the card says while the answer to task is coming.
func (s *Screen) working(task string) string {
	if task == "explain" {
		return s.text.Explaining
	}
	return s.text.Translating
}

// closeSelection closes the card, and drops the answer still to come.
func (s *Screen) closeSelection() {
	s.sel = selection{ticket: s.sel.ticket + 1}
}

// selectionCard is the card over the foot of the reader, an AI card (design BAICard) raised as a float with the popup
// elevation: the task and the start of the text asked about, then the answer, the answer coming, why there is none,
// or the word that the text is too long.
func (s *Screen) selectionCard(c *ui.Context) {
	sel := &s.sel
	if !sel.on {
		return
	}
	t := c.Theme()
	_, height := c.Size()
	name, icon := s.text.TranslateSelection, icons.Languages
	if sel.task == "explain" {
		name, icon = s.text.ExplainSelection, icons.Sparkles
	}
	if sel.aiOff {
		name, icon = s.text.AIOffTitle, icons.Sparkles
	}
	edge := theme.Px(t, tokens.Space4)
	ui.Row(c).Absolute().Left(0).Right(0).Bottom(edge).Padding(0, edge).Justify(ui.Center).PassThrough().Children(func() {
		card := ui.Column(c).FillWidth().MaxWidth(s.column(t)).Gap(theme.Px(t, tokens.Space2)).
			Padding(theme.Px(t, tokens.Space3), theme.Px(t, tokens.Space4)).Radius(theme.Px(t, tokens.RadiusCard)).
			Background(theme.RaisedFill(t)).Role(ui.RoleGroup).Label(name)
		theme.Shadow(t, card, theme.Elevation(t).Popup)
		card.Children(func() {
			ui.Row(c).FillWidth().MinHeight(theme.Px(t, tokens.RowDense)).AlignItems(ui.Center).
				Gap(theme.Px(t, tokens.Space1_5)).Children(func() {
				ui.Icon(c, icon).FontSize(theme.IconSmall(t)).TextColor(t.Accent)
				ui.Text(c, name).FontWeight(tokens.TextLabel.Weight).NoWrap()
				quoted := ""
				if !sel.aiOff {
					quoted = "“" + quote(sel.text) + "”"
				}
				ui.Text(c, quoted).FontSize(theme.Px(t, tokens.TextMeta.Size.En)).TextColor(t.TextMuted).SingleLine().
					MinWidth(0).Shrink(1).Grow(1)
				if (theme.Link{Text: s.text.Close, Muted: true}).Draw(c).Clicked() {
					s.closeSelection()
				}
			})
			switch {
			case sel.working:
				ui.Row(c).AlignItems(ui.Center).Gap(theme.Px(t, tokens.Space1_5)).Children(func() {
					s.status(c, s.working(sel.task), true)
				})
			case sel.failed:
				s.failure(c, sel.code, func() { s.ask(sel.task, sel.text) })
			case sel.aiOff:
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(theme.Px(t, tokens.Space2)).Role(ui.RoleStatus).Children(func() {
					ui.Text(c, s.text.SelectionAIOff).FontSize(theme.Px(t, tokens.TextMeta.Size.En)).TextColor(theme.Text2(t)).
						Wrap().Grow(1).MinWidth(0)
					if (theme.Link{Text: s.text.OpenSettings}).Draw(c).Clicked() && s.deps.OpenSettings != nil {
						s.deps.OpenSettings()
					}
				})
			case sel.long:
				ui.Column(c).Role(ui.RoleStatus).Children(func() {
					s.cardText(c, s.text.SelectLess, t.Text).FontWeight(tokens.TextLabel.Weight)
					ui.Text(c, s.text.SelectLessHint).FontSize(theme.Px(t, tokens.TextMeta.Size.En)).TextColor(theme.Text2(t)).Wrap()
				})
			default:
				ui.Scroll(c).FillWidth().MaxHeight(float32(height) * 0.4).Children(func() {
					ui.Column(c).FillWidth().Selectable().Children(func() { s.cardText(c, sel.answer, theme.Prose(t)) })
				})
				ui.Row(c).AlignItems(ui.Center).Gap(theme.Px(t, tokens.Space2)).Children(func() {
					if (theme.Button{Small: true, Text: s.text.CopyAnswer}).Draw(c).Clicked() {
						c.WriteClipboard(sel.answer)
						s.announce = s.text.Copied
					}
					if sel.model != "" {
						s.status(c, strings.ReplaceAll(s.text.By, "{model}", sel.model), false)
					}
				})
			}
		})
	})
}

// cardText is the text of a card in body, under its head.
func (s *Screen) cardText(c *ui.Context, text string, color ui.Color) ui.Element {
	t := c.Theme()
	return ui.Text(c, text).FontSize(theme.Px(t, tokens.TextBody.Size.En)).LineHeight(cardLine(text)).TextColor(color).Wrap()
}

// quote is the start of text for the head of the card: its first characters, on one line.
func quote(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(text) <= quoteLength {
		return text
	}
	return string([]rune(text)[:quoteLength]) + "…"
}
