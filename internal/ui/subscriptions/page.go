package subscriptions

import (
	"cmp"
	"context"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/ui/icons"
	"github.com/yangflow/oriel/internal/ui/locale"
	"github.com/yangflow/oriel/internal/ui/theme"
	"github.com/yangflow/oriel/internal/ui/theme/tokens"
)

// paging is the page of one subscription in Settings → Subscriptions. A select or a switch applies at once; a text box
// waits for the Save of its own row.
type paging struct {
	open     bool
	e        editing
	applying bool // a select or a switch is being applied
	prompts  bool // the Custom prompts disclosure is open
	typing   bool // a text box had the focus at the last frame
}

// OpenSource shows the page of src in Settings → Subscriptions.
func (s *Screen) OpenSource(src client.Source) {
	if s.api == nil {
		return
	}
	s.manage.cursor = src.ID
	s.pg = paging{open: true, e: editingOf(src)}
	e := &s.pg.e
	e.extract, e.headers = false, false
	e.proxyOpen = true
	s.pg.prompts = src.PromptSummary != "" || src.PromptTranslate != ""
	s.loadFormSettings(e)
}

// Back leaves the page of a subscription for the list, with the focus on its row.
func (s *Screen) Back() {
	if s.pg.open {
		s.manage.focusRow = s.pg.e.src.ID
	}
	s.closePage()
}

func (s *Screen) closePage() {
	s.pg = paging{}
	s.form = &s.edit
}

// textGroups are the parts of the page that wait for their own Save, in the order of the page.
const (
	partTitle = iota
	partExtract
	partHeaders
	partProxy
	partMute
	partPrompts
)

// dirty reports whether the text of part differs from what the server has.
func (e *editing) dirty(part int) bool {
	src := e.src
	switch part {
	case partTitle:
		return strings.TrimSpace(e.title) != src.Title
	case partExtract:
		return strings.TrimSpace(e.selector) != src.ExtractSelector || strings.TrimSpace(e.referer) != src.ExtractReferer ||
			strings.TrimSpace(e.rewrites) != src.ExtractRewrites
	case partHeaders:
		return strings.TrimSpace(e.agent) != src.FetchUserAgent || strings.TrimSpace(e.fetchRef) != src.FetchReferer
	case partProxy:
		return e.proxyMode == proxyCustom && (e.proxyValue() != src.Proxy || e.proxyUser != "" || e.proxyPass != "" ||
			e.proxyDrop && e.proxyAuthSaved())
	case partMute:
		return e.mute != src.MuteKeywords
	case partPrompts:
		return strings.TrimSpace(e.prompts[0]) != src.PromptSummary || strings.TrimSpace(e.prompts[1]) != src.PromptTranslate
	}
	return false
}

// pageDirty reports whether a text box of the page has text that is not saved.
func (s *Screen) pageDirty() bool {
	for part := partTitle; part <= partPrompts; part++ {
		if s.pg.e.dirty(part) {
			return true
		}
	}
	return false
}

// partPatch is what the Save of part sends.
func (e *editing) partPatch(part int) client.SourcePatch {
	var p client.SourcePatch
	trim := func(text string) *string { v := strings.TrimSpace(text); return &v }
	switch part {
	case partTitle:
		p.Title = trim(e.title)
	case partExtract:
		p.ExtractSelector, p.ExtractReferer, p.ExtractRewrites = trim(e.selector), trim(e.referer), trim(e.rewrites)
	case partHeaders:
		p.FetchUserAgent, p.FetchReferer = trim(e.agent), trim(e.fetchRef)
	case partProxy:
		proxy := e.proxyValue()
		p.Proxy = &proxy
		if e.proxyDrop && e.proxyAuthSaved() {
			p.ProxyUser, p.ProxyPassword = new(string), new(string)
		}
		if e.proxyUser != "" {
			p.ProxyUser = new(e.proxyUser)
		}
		if e.proxyPass != "" {
			p.ProxyPassword = new(e.proxyPass)
		}
	case partMute:
		p.MuteKeywords = new(e.mute)
	case partPrompts:
		p.PromptSummary, p.PromptTranslate = trim(e.prompts[0]), trim(e.prompts[1])
	}
	return p
}

// take makes the fields of part what the server now has.
func (e *editing) take(part int, nw editing) {
	switch part {
	case partTitle:
		e.title = nw.title
	case partExtract:
		e.selector, e.referer, e.rewrites = nw.selector, nw.referer, nw.rewrites
	case partHeaders:
		e.agent, e.fetchRef, e.headerErr = nw.agent, nw.fetchRef, [2]bool{}
	case partProxy:
		e.proxyMode, e.proxyURL, e.proxyUser, e.proxyPass, e.proxyDrop, e.proxyErr = nw.proxyMode, nw.proxyURL, "", "", false, false
	case partMute:
		e.mute = nw.mute
	case partPrompts:
		e.prompts = nw.prompts
	}
}

// follow sets *field to now when it still holds was: what the server changed reaches the page, what was typed stays.
func follow[T comparable](field *T, was, now T) {
	if *field == was {
		*field = now
	}
}

// adopt makes src the subscription the page shows, keeping what was changed on the page and not sent yet.
func (e *editing) adopt(src client.Source) {
	was, now := editingOf(e.src), editingOf(src)
	follow(&e.title, was.title, now.title)
	follow(&e.kind, was.kind, now.kind)
	follow(&e.interval, was.interval, now.interval)
	follow(&e.keep, was.keep, now.keep)
	follow(&e.target, was.target, now.target)
	follow(&e.pace, was.pace, now.pace)
	follow(&e.dailyCap, was.dailyCap, now.dailyCap)
	follow(&e.paused, was.paused, now.paused)
	follow(&e.hide, was.hide, now.hide)
	follow(&e.folder, was.folder, now.folder)
	follow(&e.mute, was.mute, now.mute)
	follow(&e.selector, was.selector, now.selector)
	follow(&e.referer, was.referer, now.referer)
	follow(&e.rewrites, was.rewrites, now.rewrites)
	follow(&e.agent, was.agent, now.agent)
	follow(&e.fetchRef, was.fetchRef, now.fetchRef)
	follow(&e.proxyMode, was.proxyMode, now.proxyMode)
	follow(&e.proxyURL, was.proxyURL, now.proxyURL)
	follow(&e.auto, was.auto, now.auto)
	follow(&e.titles, was.titles, now.titles)
	follow(&e.prompts, was.prompts, now.prompts)
	e.src = src
}

// choices is the patch of the selects and switches of the page that differ from what the server has. A custom proxy
// waits for the Save of its boxes.
func (e *editing) choices() (client.SourcePatch, bool) {
	var p client.SourcePatch
	src, changed := e.src, false
	if kind := kinds[e.kind]; kind != cmp.Or(src.Kind, kinds[0]) {
		p.Kind, changed = &kind, true
	}
	if e.folder != src.FolderID {
		p.FolderID, changed = new(e.folder), true
	}
	if e.interval != intervalChoice(src) {
		if e.interval == 0 {
			p.FetchIntervalAuto = new(true)
		} else {
			p.FetchIntervalSec = new(e.interval)
		}
		changed = true
	}
	if target := openTargets[e.target]; target != cmp.Or(src.OpenTarget, openTargets[0]) {
		p.OpenTarget, changed = &target, true
	}
	if int(e.keep) != src.KeepDays {
		p.KeepDays, changed = new(int(e.keep)), true
	}
	if int(e.pace) != src.PaceSec {
		p.PaceSec, changed = new(int(e.pace)), true
	}
	if int(e.dailyCap) != src.DailyCap {
		p.DailyCap, changed = new(int(e.dailyCap)), true
	}
	if e.paused != src.Paused {
		p.Paused, changed = new(e.paused), true
	}
	if e.hide != src.HideFromAll {
		p.HideFromAll, changed = new(e.hide), true
	}
	if e.proxyMode != proxyCustom && e.proxyValue() != src.Proxy {
		p.Proxy, changed = new(e.proxyValue()), true
	}
	if e.auto[0] != src.AutoSummary {
		p.AutoSummary, changed = new(e.auto[0]), true
	}
	if e.auto[1] != src.AutoTranslate {
		p.AutoTranslate, changed = new(e.auto[1]), true
	}
	if e.titles != src.TranslateTitles {
		p.TranslateTitles, changed = new(e.titles), true
	}
	return p, changed
}

// apply sends the selects and switches changed at the last frame. A refusal puts them back as the server has them.
func (s *Screen) apply() {
	pg := &s.pg
	patch, changed := pg.e.choices()
	if !changed || pg.applying || pg.e.busy || s.api == nil {
		return
	}
	pg.applying = true
	gen, api, id := s.gen, s.api, pg.e.src.ID
	s.deps.Go(func() func() {
		src, err := api.UpdateSource(context.Background(), id, patch)
		return func() {
			if gen != s.gen || s.expired(err) || !pg.open || pg.e.src.ID != id {
				return
			}
			pg.applying = false
			switch {
			case err == nil:
				pg.e.adopt(src)
				s.deps.Changed(src)
			case code(err) == "not_found":
				s.gone(id)
			default:
				was := pg.e
				now := editingOf(pg.e.src)
				pg.e.kind, pg.e.folder, pg.e.interval, pg.e.target, pg.e.keep = now.kind, now.folder, now.interval, now.target, now.keep
				pg.e.pace, pg.e.dailyCap, pg.e.paused, pg.e.hide = now.pace, now.dailyCap, now.paused, now.hide
				pg.e.auto, pg.e.titles = now.auto, now.titles
				if was.proxyMode != proxyCustom {
					pg.e.proxyMode = now.proxyMode
				}
				s.toast = s.problem(err)
			}
		}
	})
}

// savePart sends the text of part.
func (s *Screen) savePart(part int) {
	pg := &s.pg
	e := &pg.e
	if e.busy || pg.applying || s.api == nil || !e.dirty(part) {
		return
	}
	if part == partProxy && e.proxyValue() == "" {
		e.proxyErr = true
		s.announce = s.text.ProxyError
		return
	}
	e.busy, e.formErr = true, ""
	patch := e.partPatch(part)
	gen, api, id := s.gen, s.api, e.src.ID
	s.deps.Go(func() func() {
		src, err := api.UpdateSource(context.Background(), id, patch)
		return func() {
			if gen != s.gen || s.expired(err) || !pg.open || e.src.ID != id {
				return
			}
			e.busy = false
			switch {
			case err == nil:
				e.take(part, editingOf(src))
				e.adopt(src)
				s.deps.Changed(src)
				s.announce = s.text.Saved
			case code(err) == "not_found":
				s.gone(id)
			case code(err) == "invalid_fetch_user_agent" || code(err) == "invalid_fetch_referer":
				e.headerErr = [2]bool{code(err) == "invalid_fetch_user_agent", code(err) == "invalid_fetch_referer"}
				s.announce = s.text.HeaderUaError
				if e.headerErr[1] {
					s.announce = s.text.HeaderRefererError
				}
			case code(err) == "invalid_proxy":
				e.proxyErr = true
				s.announce = s.text.ProxyError
			default:
				e.formErr = s.saveProblem(err)
				s.announce = e.formErr
			}
		}
	})
}

// sync takes what the timeline last loaded of the subscription, while nothing of the page is under way.
func (s *Screen) sync() {
	pg := &s.pg
	if pg.applying || pg.e.busy || s.deps.Sources == nil {
		return
	}
	for _, src := range s.deps.Sources() {
		if src.ID == pg.e.src.ID {
			if src != pg.e.src {
				pg.e.adopt(src)
			}
			return
		}
	}
	s.closePage()
	s.toast = s.text.Gone
}

// page draws the page of one subscription: its head, then General, Fetching, Timeline, Summaries and translations and
// Delete.
func (s *Screen) page(c *ui.Context) {
	t := c.Theme()
	pg := &s.pg
	s.sync()
	if !pg.open {
		s.listPage(c)
		return
	}
	e := &pg.e
	e.open = true
	s.form = e
	pg.typing = false
	defer func() { s.form = &s.edit }()
	ui.Column(c).Key(e.src.ID).FillWidth().Children(func() {
		s.pageHead(c)
		pageTitle(c, s.text.PageGeneral, theme.Px(t, tokens.Space6+tokens.Space1))
		pageCard(c, func() { s.titleRow(c) }, func() { s.selectRow(c, s.kindRow) }, when(len(s.folders()) > 0, func() {
			s.selectRow(c, s.folderRow)
		}), func() {
			s.pageSwitch(c, "pause", &e.paused, s.text.PauseLabel, s.text.PauseHelp)
		}, func() {
			s.pageSwitch(c, "hide", &e.hide, s.text.HideLabel, s.text.HideHelp)
		})
		pageTitle(c, s.text.PageFetching, theme.Px(t, tokens.Space6))
		pageCard(c, func() { s.selectRow(c, s.everyRow) }, func() { s.extractRow(c) }, when(e.src.URL != "", func() {
			s.headersRow(c)
		}), func() { s.proxyRow(c) })
		pageTitle(c, s.text.PageTimeline, theme.Px(t, tokens.Space6))
		pageCard(c, func() { s.selectRow(c, s.openRow) }, func() { s.selectRow(c, s.keepRow) }, func() { s.selectRow(c, s.paceRow) },
			func() { s.selectRow(c, s.capRow) }, func() { s.muteRow(c) })
		pageTitle(c, s.text.AIGroup, theme.Px(t, tokens.Space6))
		pageCard(c, when(e.aiOff, func() { s.aiOffRow(c) }), func() {
			s.pageSwitch(c, "summary", &e.auto[0], s.text.AutoSummary, s.text.AutoHelp)
		}, func() {
			s.pageSwitch(c, "translate", &e.auto[1], s.text.AutoTranslate, s.text.AutoHelp)
		}, func() {
			s.pageSwitch(c, "titles", &e.titles, s.text.TranslateTitles, s.text.TranslateTitlesHelp)
		}, func() { s.promptsRow(c) })
		pageTitle(c, s.text.PageDelete, theme.Px(t, tokens.Space6))
		pageCard(c, func() {
			s.row(c, s.text.DeleteSubscription, s.text.DeleteSubscriptionHelp, func() {
				if (theme.Button{Style: theme.Danger, Small: true, Text: s.text.Delete, Disabled: e.busy,
					Label: s.fill(s.text.DeleteSource, Name(e.src))}).Draw(c).Clicked() {
					s.AskDelete(e.src)
				}
			})
		})
	})
	s.apply()
}

// pageTitle is the title over a card of the page, as on the other pages of Settings.
func pageTitle(c *ui.Context, text string, top float32) {
	t := c.Theme()
	ui.Text(c, text).FontSize(theme.Px(t, tokens.TextTitle.Size.En)).FontWeight(tokens.TextTitle.Weight).
		Margin(top, theme.Px(t, tokens.Space1), theme.Px(t, tokens.Space3)).Role(ui.RoleHeading).Level(2)
}

// pageCard is a card of the page: the Surface plate with a hairline between its rows. A nil row is left out; each row
// keeps its key when one before it comes or goes.
func pageCard(c *ui.Context, rows ...func()) {
	t := c.Theme()
	ui.Column(c).FillWidth().Radius(theme.Px(t, tokens.RadiusCard)).Background(theme.Surface(t)).
		Padding(theme.Px(t, tokens.Space1), theme.Px(t, tokens.Space4)).Children(func() {
		first := true
		for i, row := range rows {
			if row == nil {
				continue
			}
			ui.Column(c).Key(i).FillWidth().Children(func() {
				if !first {
					ui.Box(c).FillWidth().Height(tokens.Hairline).Background(t.Border)
				}
				first = false
				ui.Column(c).FillWidth().MinHeight(theme.Px(t, tokens.RowCompact)).Justify(ui.Center).
					PaddingY(theme.Px(t, tokens.Space3)).Children(row)
			})
		}
	})
}

// when is row while on holds, else nil, which pageCard leaves out.
func when(on bool, row func()) func() {
	if on {
		return row
	}
	return nil
}

// row is a settings row: the name over its help at the left, the control at the right.
func (s *Screen) row(c *ui.Context, name, helpText string, control func()) {
	t := c.Theme()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(theme.Px(t, tokens.Space3)).Children(func() {
		ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(theme.Px(t, tokens.Space0_5)).Children(func() {
			ui.Text(c, name).FontSize(theme.Px(t, tokens.TextLabel.Size.En)).FontWeight(tokens.TextLabel.Weight)
			if helpText != "" {
				help(c, helpText)
			}
		})
		control()
	})
}

// pick is a select row of the page: its name, help, value and choices.
type pick struct {
	value   *int64
	options []int64
	name    string
	help    string
	shown   string
	names   func(int64) string
	muted   bool
	done    func() // takes the value chosen, for a value that is not an int64 of the form
}

func (s *Screen) selectRow(c *ui.Context, of func() pick) {
	t := c.Theme()
	p := of()
	s.row(c, p.name, p.help, func() {
		ui.Box(c).Width(theme.Px(t, tokens.MenuMin+tokens.Space12)).Shrink(0).Children(func() {
			s.selectTrigger(c, p.value, p.options, p.name, p.shown, p.names, s.form.busy, p.muted)
		})
	})
	if p.done != nil {
		p.done()
	}
}

func (s *Screen) kindRow() pick {
	e := s.form
	value := int64(e.kind)
	options := make([]int64, len(kinds))
	for i := range kinds {
		options[i] = int64(i)
	}
	return pick{value: &value, options: options, name: s.text.ShowAs, shown: s.kindName(kinds[e.kind]),
		names: func(i int64) string { return s.kindName(kinds[i]) }, done: func() { e.kind = int(value) }}
}

func (s *Screen) folderRow() pick {
	e := s.form
	options := []int64{0}
	for _, f := range s.folders() {
		options = append(options, f.ID)
	}
	return pick{value: &e.folder, options: options, name: s.text.Folder, help: s.text.FolderHelp, shown: s.folderName(e.folder),
		names: s.folderName, muted: e.folder == 0}
}

func (s *Screen) everyRow() pick {
	e := s.form
	options := intervals
	if !contains(options, e.interval) {
		options = sortedWith(options, e.interval)
	}
	shown := s.every(e.interval)
	if e.interval == 0 && e.src.FetchIntervalAuto && e.src.FetchIntervalSec > 0 {
		shown = put(s.text.AutoEvery, "every", s.every(e.src.FetchIntervalSec))
	}
	return pick{value: &e.interval, options: options, name: s.text.Every, help: s.text.EveryHelp, shown: shown, names: s.every}
}

func (s *Screen) openRow() pick {
	e := s.form
	return pick{value: &e.target, options: []int64{0, 1, 2}, name: s.text.OpenTarget, help: s.text.OpenTargetHelp,
		shown: s.openName(e.target), names: s.openName}
}

func (s *Screen) keepRow() pick {
	e := s.form
	options := keeps
	if !contains(options, e.keep) {
		options = append(sortedWith(keeps[:len(keeps)-1], e.keep), keeps[len(keeps)-1])
	}
	names := func(v int64) string {
		if v != 0 {
			return s.keepName(v)
		}
		global := s.text.KeepForever
		if e.global > 0 {
			global = s.keepName(int64(e.global))
		}
		return put(s.text.KeepFollowing, "keep", global)
	}
	return pick{value: &e.keep, options: options, name: s.text.Keep, help: s.text.KeepHelp, shown: names(e.keep), names: names}
}

func (s *Screen) paceRow() pick {
	e := s.form
	options := paces
	if !contains(options, e.pace) {
		options = sortedWith(options, e.pace)
	}
	return pick{value: &e.pace, options: options, name: s.text.Pace, help: s.text.PaceHelp, shown: s.paceName(e.pace), names: s.paceName}
}

func (s *Screen) capRow() pick {
	e := s.form
	options := caps
	if !contains(options, e.dailyCap) {
		options = sortedWith(options, e.dailyCap)
	}
	return pick{value: &e.dailyCap, options: options, name: s.text.Cap, help: s.text.CapHelp, shown: s.capName(e.dailyCap), names: s.capName}
}

func contains(options []int64, v int64) bool {
	for _, o := range options {
		if o == v {
			return true
		}
	}
	return false
}

// sortedWith is options with v added, in order.
func sortedWith(options []int64, v int64) []int64 {
	out := make([]int64, 0, len(options)+1)
	added := false
	for _, o := range options {
		if !added && v < o {
			out, added = append(out, v), true
		}
		out = append(out, o)
	}
	if !added {
		out = append(out, v)
	}
	return out
}

// pageSwitch is a switch row of the page, which applies at once.
func (s *Screen) pageSwitch(c *ui.Context, key string, value *bool, name, helpText string) {
	s.row(c.Key(key), name, helpText, func() {
		sw := ui.Switch(c, value).Label(name).Disabled(s.form.busy)
		if helpText != "" {
			sw.Description(helpText)
		}
	})
}

// saveButton is the Save of a text part: quiet until the part changed, primary after.
func (s *Screen) saveButton(c *ui.Context, part int, label string) {
	e := s.form
	dirty := e.dirty(part)
	style := theme.Secondary
	if dirty {
		style = theme.Primary
	}
	if (theme.Button{Style: style, Small: true, Text: s.text.Save, Label: label, Disabled: !dirty || e.busy}).Draw(c).Clicked() {
		s.savePart(part)
	}
}

// typed notes that a text box of the page has the focus, so Escape first takes the focus away.
func (s *Screen) typed(el ui.Element) {
	if el.FocusWithin() {
		s.pg.typing = true
	}
}

func (s *Screen) titleRow(c *ui.Context) {
	t := c.Theme()
	e := s.form
	ui.Column(c.Key("title-row")).FillWidth().Gap(theme.Px(t, tokens.Space1)).Children(func() {
		label(c, s.text.Title)
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(theme.Px(t, tokens.Space2)).Children(func() {
			b := box(c, "title", nil, false, func() {
				in := ui.TextInputBase(c, &e.title).Grow(1).MinWidth(0).FocusRing(false).Label(s.text.Title).
					Placeholder(Name(e.src)).Description(s.text.TitleHelp).Disabled(e.busy)
				if in.Submitted() {
					s.savePart(partTitle)
				}
			})
			b.Grow(1).MinWidth(0)
			s.typed(b)
			s.saveButton(c, partTitle, s.text.SaveTitle)
		})
		help(c, s.text.TitleHelp)
	})
}

// disclosure is a row that opens the fields under it: the name and help at the left, the value and a chevron at the
// right.
func (s *Screen) disclosure(c *ui.Context, key string, open *bool, name, helpText, value string) {
	t := c.Theme()
	head := theme.Fade(ui.ButtonBase(c).Key(key).FillWidth().AlignItems(ui.Center).Gap(theme.Px(t, tokens.Space3)).
		Role(ui.RoleDisclosure).Expanded(*open).Label(name).Description(helpText))
	head.Children(func() {
		ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(theme.Px(t, tokens.Space0_5)).Children(func() {
			ui.Text(c, name).FontSize(theme.Px(t, tokens.TextLabel.Size.En)).FontWeight(tokens.TextLabel.Weight)
			help(c, helpText)
		})
		ui.Text(c, value).FontSize(theme.Px(t, tokens.TextBody.Size.En)).TextColor(t.TextMuted).SingleLine().Shrink(1)
		chevron := icons.ChevronRight
		if *open {
			chevron = icons.ChevronDown
		}
		ui.Icon(c, chevron).FontSize(theme.IconSmall(t)).TextColor(t.TextMuted)
	})
	if head.Clicked() {
		*open = !*open
	}
}

// opened is the part of a disclosure row under it: a hairline, then the fields, 14 above and below.
func (s *Screen) opened(c *ui.Context, fields func()) {
	t := c.Theme()
	ui.Box(c).FillWidth().Height(tokens.Hairline).Background(t.Border).Margin(theme.Px(t, tokens.Space3), 0)
	col := ui.Column(c).FillWidth().Gap(theme.Px(t, tokens.Space2)).PaddingY(theme.Px(t, tokens.Space0_5)).Children(fields)
	s.typed(col)
}

func (s *Screen) extractRow(c *ui.Context) {
	t := c.Theme()
	e := s.form
	value := s.text.Automatic
	if e.src.ExtractSelector != "" || e.src.ExtractReferer != "" || e.src.ExtractRewrites != "" {
		value = s.text.Custom
	}
	s.disclosure(c, "extract", &e.extract, s.text.Extract, s.text.ExtractRowHelp, value)
	if !e.extract {
		return
	}
	s.opened(c, func() {
		s.extractFields(c)
		if e.formErr != "" {
			errorLine(c, e.formErr, "")
		}
		ui.Row(c).FillWidth().Justify(ui.End).Gap(theme.Px(t, tokens.Space2)).Children(func() {
			s.saveButton(c, partExtract, s.text.SaveExtract)
		})
	})
}

func (s *Screen) headersRow(c *ui.Context) {
	t := c.Theme()
	e := s.form
	value := s.text.None
	if e.src.FetchUserAgent != "" || e.src.FetchReferer != "" {
		value = s.text.Custom
	}
	s.disclosure(c, "headers", &e.headers, s.text.Headers, s.text.HeadersRowHelp, value)
	if !e.headers {
		return
	}
	s.opened(c, func() {
		s.headerFields(c)
		ui.Row(c).FillWidth().Justify(ui.End).Gap(theme.Px(t, tokens.Space2)).Children(func() {
			s.saveButton(c, partHeaders, s.text.SaveHeaders)
		})
	})
}

func (s *Screen) proxyRow(c *ui.Context) {
	t := c.Theme()
	e := s.form
	names := map[int64]string{proxyFollow: s.text.ProxyFollow, proxyDirect: s.text.ProxyDirect, proxyCustom: s.text.ProxyCustom}
	s.selectRow(c, func() pick {
		return pick{value: &e.proxyMode, options: []int64{proxyFollow, proxyDirect, proxyCustom}, name: s.text.Proxy,
			help: s.text.ProxyRowHelp, shown: names[e.proxyMode], names: func(v int64) string { return names[v] }}
	})
	if e.proxyMode != proxyCustom {
		return
	}
	s.opened(c, func() {
		s.proxyFields(c)
		ui.Row(c).FillWidth().Justify(ui.End).Gap(theme.Px(t, tokens.Space2)).Children(func() {
			s.saveButton(c, partProxy, s.text.SaveProxy)
		})
	})
}

func (s *Screen) muteRow(c *ui.Context) {
	t := c.Theme()
	e := s.form
	ui.Column(c.Key("mute-row")).FillWidth().Gap(theme.Px(t, tokens.Space1)).Children(func() {
		label(c, s.text.Mute)
		area := ui.TextArea(c, &e.mute).FillWidth().Lines(2, 6).Label(s.text.Mute).Description(s.text.MuteHelp).Disabled(e.busy)
		if area.Focused() {
			area.Background(theme.RaisedFill(t))
			s.pg.typing = true
		}
		help(c, s.text.MuteHelp)
		s.syncMuted()
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(theme.Px(t, tokens.Space2)).Children(func() {
			text := ""
			if e.known && strings.TrimSpace(e.mute) != "" {
				text = s.text.MuteNone
				if e.hidden > 0 {
					text = s.count(s.text.MuteHidden, e.hidden)
				}
			}
			ui.Text(c, text).FontSize(theme.Px(t, tokens.TextMeta.Size.En)).FontFeatures("tnum").TextColor(t.TextMuted).Grow(1).
				MinWidth(0).Wrap().Role(ui.RoleStatus)
			s.saveButton(c, partMute, s.text.SaveMute)
		})
	})
}

func (s *Screen) aiOffRow(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(theme.Px(t, tokens.Space2)).Children(func() {
		ui.Icon(c, icons.Sparkles).FontSize(theme.IconSmall(t)).TextColor(t.TextMuted)
		ui.Text(c, s.text.AIOff).Grow(1).Shrink(1).MinWidth(0).FontSize(theme.Px(t, tokens.TextMeta.Size.En)).TextColor(t.TextMuted).
			Wrap().Role(ui.RoleStatus)
		if (theme.Button{Small: true, Text: s.text.OpenSettings}).Draw(c).Clicked() && s.deps.OpenSettings != nil {
			s.deps.OpenSettings()
		}
	})
}

func (s *Screen) promptsRow(c *ui.Context) {
	t := c.Theme()
	e := s.form
	value := s.text.UsingSettings
	if e.src.PromptSummary != "" || e.src.PromptTranslate != "" {
		value = s.text.Custom
	}
	s.disclosure(c, "prompts", &s.pg.prompts, s.text.Prompts, "", value)
	if !s.pg.prompts {
		return
	}
	s.opened(c, func() {
		for i, text := range [2]string{s.text.PromptSummary, s.text.PromptTranslate} {
			ui.Column(c.Key(text)).FillWidth().Gap(theme.Px(t, tokens.Space1)).Children(func() {
				label(c, text)
				area := ui.TextArea(c, &e.prompts[i]).FillWidth().Lines(3, 8).Font("monospace").FontSize(theme.Px(t, tokens.TextMeta.Size.En)).
					Label(text).Placeholder(s.text.PromptPlaceholder).Disabled(e.busy)
				if area.Focused() {
					area.Background(theme.RaisedFill(t))
				}
			})
		}
		ui.Row(c).FillWidth().Justify(ui.End).Children(func() { s.saveButton(c, partPrompts, s.text.SavePrompts) })
	})
}

// pageHead is the head card on surface, radius-card and space-4 inside: the tile-l glyph, the name over the address
// and Refresh, or Try again with rotate-ccw while fetching fails. Under them a failing or removed subscription has its
// strip; a healthy or paused one its health line. The RSSHub instance line and the moved card follow.
func (s *Screen) pageHead(c *ui.Context) {
	t := c.Theme()
	e := s.form
	src := e.src
	where := cmp.Or(src.URL, src.RSSHubPath)
	ui.Column(c).FillWidth().Gap(theme.Px(t, tokens.Space4)).Padding(theme.Px(t, tokens.Space4)).Radius(theme.Px(t, tokens.RadiusCard)).
		Background(theme.Surface(t)).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(theme.Px(t, tokens.Space4)).Children(func() {
			s.rowGlyph(c, src, tokens.TileL)
			ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(theme.Px(t, tokens.Space0_5)).Children(func() {
				ui.Text(c, Name(src)).FontSize(theme.Px(t, tokens.TextTitle.Size.En)).FontWeight(tokens.TextTitle.Weight).SingleLine().
					Role(ui.RoleHeading).Level(2)
				ui.Column(c).FillWidth().Selectable().Children(func() {
					ui.Text(c, where).Font("monospace").FontSize(theme.Px(t, tokens.TextMeta.Size.En)).TextColor(t.TextMuted).SingleLine()
				})
			})
			icon := icons.RefreshCw
			if src.LastError != "" {
				icon = icons.RotateCcw
			}
			if !src.Paused && (theme.Button{Icon: icon, Text: s.refreshText(src), Key: "page-refresh"}).Draw(c).Clicked() {
				s.Refresh(src)
			}
		})
		switch health := healthOf(src); health {
		case healthFailing, healthGone:
			s.failStrip(c, src, health)
		default:
			text, _ := s.healthText(src, s.deps.Now())
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(theme.Px(t, tokens.Space1_5)).Role(ui.RoleStatus).Label(text).Children(func() {
				icon := icons.Check
				if src.Paused {
					icon = icons.Pause
				}
				ui.Icon(c, icon).FontSize(theme.IconSmall(t)).TextColor(t.TextMuted)
				ui.Text(c, text).FontSize(theme.Px(t, tokens.TextMeta.Size.En)).TextColor(t.TextMuted).Shrink(1).Wrap()
			})
		}
		s.servedBy(c)
		s.movedCard(c)
	})
}

// failStrip is the strip of a failing or removed subscription, on its meaning color at the wash alpha over paper:
// the pill, why with when it was tried and will be again, and when it last succeeded in muted. A screen reader hears
// it whole, with the server's own words.
func (s *Screen) failStrip(c *ui.Context, src client.Source, health health) {
	t := c.Theme()
	now := s.deps.Now()
	ago := func(ms int64) string { return locale.Ago(s.lang, now, time.UnixMilli(ms)) }
	tag, _, ink, _ := s.statusLine(t, src)
	icon := icons.TriangleAlert
	if health == healthGone {
		icon = icons.CircleAlert
	}
	why := FailureReason(s.lang, src, s.text.Blocked, s.text.BlockedPublic, s.text.FeedRemoved)
	if src.LastFetchedAt != 0 {
		tried := strings.ReplaceAll(s.text.StripTriedOnly, "{t}", ago(src.LastFetchedAt))
		if in := time.UnixMilli(src.NextFetchAt).Sub(now); health == healthFailing && in >= time.Minute {
			tried = strings.NewReplacer("{t}", ago(src.LastFetchedAt), "{next}", s.span(in)).Replace(s.text.StripTried)
		}
		why += s.text.Separator + tried
	}
	last := ""
	if src.LastSuccessAt != 0 {
		last = strings.ReplaceAll(s.text.LastSuccess, "{t}", ago(src.LastSuccessAt))
	}
	label := tag + s.text.Separator + FailureDetail(s.lang, src, why)
	if last != "" {
		label += s.text.Separator + last
	}
	size := theme.Px(t, tokens.TextMeta.Size.En)
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(theme.Px(t, tokens.Space3)).
		Padding(theme.Px(t, tokens.Space3), theme.Px(t, tokens.Space4)).Radius(theme.Px(t, tokens.RadiusControl)).
		Background(ink.Alpha(tokens.OpacityWash).Over(t.Background)).Role(ui.RoleStatus).Label(label).Children(func() {
		statusPill(c, icon, tag, ink)
		ui.Text(c, why).FontSize(size).Grow(1).Basis(0).MinWidth(0).Wrap()
		if last != "" {
			ui.Text(c, last).FontSize(size).TextColor(t.TextMuted).Shrink(0).NoWrap()
		}
	})
}
