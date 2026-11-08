package settings

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/ui/icons"
	"github.com/yangflow/oriel/internal/ui/locale"
	"github.com/yangflow/oriel/internal/ui/theme"
	"github.com/yangflow/oriel/internal/ui/theme/tokens"
)

// deletedToast is the ID of the toast of a deleted rule.
const deletedToast = "settings.rule-deleted"

// maxRules is how many rules the server keeps; the button to add one is off at that many.
const maxRules = 50

// maxRuleSources is how many subscriptions one rule can be limited to.
const maxRuleSources = 50

// mutedWait is how long the muted keywords rest before the entries they would hide are counted.
const mutedWait = 400 * time.Millisecond

// ruleKinds are the kinds a rule can be limited to, "" for any.
var ruleKinds = []string{"", "article", "video", "social", "image", "forum"}

// ruleForm is the dialog that adds a rule or edits one.
type ruleForm struct {
	open     bool
	id       int64   // the rule being edited, 0 for a new one
	enabled  bool    // kept as it was when editing
	sources  []int64 // the subscriptions chosen, none for every one
	subsOpen bool    // the list of subscriptions is open
	kind     string
	keywords string
	exclude  string
	author   string
	star     bool
	read     bool
	focus    bool
	saving   bool
	run      int // counts saves, so the answer of one the dialog was closed on is dropped
	scroll   ui.ScrollState

	condErr, actErr, keysErr, authErr, formErr string

	seen     string    // the conditions the last frame saw, to tell when they change
	seenAt   time.Time // when they changed
	asked    string    // the conditions whose count was asked for last
	count    int
	counted  bool // count is the count of asked
	countRun int  // counts the questions, so the answer of an old one is dropped
}

// deletion is a rule just deleted, which the toast offers to put back.
type deletion struct {
	rule  client.Rule
	shown bool
}

// rulesPage is the state of the Rules group: the rules, the dialog on one and the muted keywords.
type rulesPage struct {
	state   int
	loads   int
	list    []client.Rule
	sources []client.Source // by title
	dialog  ruleForm
	deleted *deletion

	muted      string // the muted keywords as typed
	mutedState status
	invalid    bool
	focus      bool
	saving     bool
	seen       string    // the cleaned keywords the last frame saw, to tell when they change
	seenAt     time.Time // when they changed
	asked      string    // the cleaned keywords whose count was asked for last
	count      int
	counted    bool // count is the count of asked
	counting   bool
	countRun   int
}

// takeMuted makes the keywords the server has the ones typed.
func (p *rulesPage) takeMuted(text string) {
	p.muted, p.mutedState, p.invalid, p.saving = text, status{}, false, false
	p.seen, p.asked, p.counted = "", "", false
}

// cleanLines is text with each line trimmed and the empty ones dropped.
func cleanLines(text string) string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

func (s *Screen) loadRules() {
	if s.api == nil {
		return
	}
	p := &s.rules
	p.loads++
	p.state = loading
	gen, run, api := s.gen, p.loads, s.api
	s.deps.Go(func() func() {
		list, err := api.Rules(context.Background())
		var sources []client.Source
		if err == nil {
			var all client.SourceList
			all, err = api.Sources(context.Background())
			sources = all.Sources
		}
		return func() {
			if gen != s.gen || run != p.loads || s.expired(err) {
				return
			}
			if err != nil {
				p.state = failed
				s.announce = s.text.RulesError
				return
			}
			slices.SortStableFunc(sources, func(a, b client.Source) int {
				return strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title))
			})
			p.state, p.list, p.sources = loaded, list, sources
		}
	})
}

// sourceNames is the subscriptions a rule is limited to: every one, the name of one, the names of two, or the number
// of more.
func (s *Screen) sourceNames(ids []int64) string {
	switch {
	case len(ids) == 0:
		return s.text.AllSubs
	case len(ids) > 2:
		return locale.Count(s.text.SubsN, "n", len(ids))
	}
	var names []string
	for _, src := range s.rules.sources {
		if slices.Contains(ids, src.ID) {
			names = append(names, src.Title)
		}
	}
	return strings.Join(names, ", ")
}

// words is the lines of text as a list of quoted words.
func (s *Screen) words(text string) string {
	lines := strings.Split(cleanLines(text), "\n")
	for i, w := range lines {
		lines[i] = "“" + w + "”"
	}
	sep := ", "
	if s.lang == "zh-Hans" {
		sep = "、"
	}
	return strings.Join(lines, sep)
}

func (s *Screen) kindName(kind string) string {
	switch kind {
	case "article":
		return s.text.ViewArticles
	case "video":
		return s.text.ViewVideos
	case "social":
		return s.text.ViewSocial
	case "image":
		return s.text.ViewImages
	case "forum":
		return s.text.ViewForum
	}
	return s.text.KindAny
}

// summary is the condition of a rule as a sentence.
func (s *Screen) summary(r client.Rule) string {
	var parts []string
	if cleanLines(r.Keywords) != "" {
		parts = append(parts, s.fill(s.text.RwKeywords, "{words}", s.words(r.Keywords)))
	}
	if cleanLines(r.Exclude) != "" {
		parts = append(parts, s.fill(s.text.RwExclude, "{words}", s.words(r.Exclude)))
	}
	if r.Author != "" {
		parts = append(parts, s.fill(s.text.RwAuthor, "{name}", r.Author))
	}
	if len(parts) == 0 {
		return s.text.RwOnly
	}
	return strings.Join(parts, " · ")
}

// scope is the subscription and the kind a rule is limited to.
func (s *Screen) scope(r client.Rule) string {
	var parts []string
	if names := s.sourceNames(r.SourceIDs); names != "" {
		parts = append(parts, names)
	}
	if r.Kind != "" {
		parts = append(parts, s.kindName(r.Kind))
	}
	return strings.Join(parts, " · ")
}

// action names what a rule does.
func (s *Screen) action(r client.Rule) (*ui.SVG, string) {
	switch {
	case r.Star && r.MarkRead:
		return icons.Star, s.text.ActStar + " + " + s.text.ActRead
	case r.Star:
		return icons.Star, s.text.ActStar
	}
	return icons.CheckCheck, s.text.ActRead
}

func (s *Screen) rulesPage(c *ui.Context) {
	t := c.Theme()
	p := &s.rules
	plate(c).Children(func() {
		if len(p.list) == 0 {
			ui.Column(c).Key("none").FillWidth().Children(func() {
				ui.Column(c).FillWidth().PaddingY(theme.Px(t, tokens.Space3)).Children(func() {
					setting(c, icons.ListFilter, false, s.text.RulesNone, func() { meta(c, s.text.RulesNoneHelp) }, nil)
				})
				hairline(c)
			})
		}
		for _, r := range p.list {
			id := strconv.FormatInt(r.ID, 10)
			ui.Column(c).Key("line" + id).FillWidth().Children(func() {
				s.ruleRow(c, r, id)
				hairline(c)
			})
		}
		ui.Row(c).Key("add").FillWidth().AlignItems(ui.Center).Gap(theme.Px(t, tokens.Space3)).
			PaddingY(theme.Px(t, tokens.Space3)).Children(func() {
			full := len(p.list) >= maxRules
			if full {
				meta(c, s.text.RulesMax).Grow(1).MinWidth(0)
			} else {
				ui.Box(c).Grow(1)
			}
			add := theme.Button{Style: theme.Secondary, Disabled: full, Icon: icons.Plus, Text: s.text.AddRule}.Draw(c)
			if add.Clicked() {
				s.openRule(nil)
			}
		})
	})
	s.mutedCard(c)
}

func (s *Screen) ruleRow(c *ui.Context, r client.Rule, id string) {
	t := c.Theme()
	summary := s.summary(r)
	ui.Row(c).Key("rule" + id).FillWidth().AlignItems(ui.Center).Gap(theme.Px(t, tokens.Space3)).
		PaddingY(theme.Px(t, tokens.Space3)).Children(func() {
		text := ui.ButtonBase(c).Grow(1).Shrink(1).MinWidth(0).Column().AlignItems(ui.Start).
			Radius(theme.Px(t, tokens.RadiusSm)).Label(summary)
		if !r.Enabled {
			text.Opacity(tokens.OpacityFaded)
		}
		text.Children(func() {
			ui.Text(c, summary).Wrap()
			icon, act := s.action(r)
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(theme.Px(t, tokens.Space1_5)).Margin(theme.Px(t, tokens.Space0_5), 0, 0, 0).
				Children(func() {
					if scope := s.scope(r); scope != "" {
						ui.Text(c, scope).FontSize(theme.Px(t, tokens.TextMeta.Size.En)).TextColor(t.TextMuted)
					}
					ui.Icon(c, icons.ArrowRight).FontSize(theme.IconSmall(t)).TextColor(theme.Text2(t))
					ui.Icon(c, icon).FontSize(theme.IconSmall(t)).TextColor(theme.Text2(t))
					ui.Text(c, act).FontSize(theme.Px(t, tokens.TextMeta.Size.En)).TextColor(t.TextMuted)
					if !r.Enabled {
						tag(c, s.text.RuleOffBadge, "", false)
					}
				})
		})
		if text.Clicked() {
			s.openRule(&r)
		}
		on := r.Enabled
		ui.Switch(c, &on).Label(s.text.RuleSwitch).Description(summary)
		if on != r.Enabled {
			s.toggleRule(r.ID, on)
		}
		more := theme.Fade(ui.ButtonBase(c).Size(theme.Px(t, tokens.Row), theme.Px(t, tokens.Row)).
			Radius(theme.Px(t, tokens.RadiusControl)).Label(s.text.RuleMore).
			Tooltip(s.text.RuleMore))
		color := t.TextMuted
		switch {
		case more.Pressed():
			more.Background(t.SurfacePressed)
			color = t.Text
		case more.Hovered():
			more.Background(t.SurfaceHover)
			color = t.Text
		}
		more.Children(func() { ui.Icon(c, icons.Ellipsis).FontSize(theme.IconMedium(t)).TextColor(color) })
		more.Menu(func(m *ui.Menu) {
			if m.Item(s.text.MenuEdit).Chosen() {
				s.openRule(&r)
			}
			if m.Item(s.text.MenuDelete).Chosen() {
				s.deleteRule(r.ID)
			}
		})
	})
}

// ruleIndex is where the rule called id is in the list, -1 for none.
func (p *rulesPage) ruleIndex(id int64) int {
	return slices.IndexFunc(p.list, func(r client.Rule) bool { return r.ID == id })
}

func (s *Screen) toggleRule(id int64, on bool) {
	p := &s.rules
	i := p.ruleIndex(id)
	if i < 0 || s.api == nil {
		return
	}
	p.list[i].Enabled = on
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		rule, err := api.SetRuleEnabled(context.Background(), id, on)
		return func() {
			if gen != s.gen || s.expired(err) {
				return
			}
			i := p.ruleIndex(id)
			if err != nil {
				if i >= 0 {
					p.list[i].Enabled = !on
				}
				s.toast = s.text.SaveFailed
				return
			}
			if i >= 0 {
				p.list[i] = rule
			}
		}
	})
}

// openRule opens the dialog on a new rule, or on r to edit it.
func (s *Screen) openRule(r *client.Rule) {
	d := ruleForm{open: true, enabled: true, focus: true}
	if r != nil {
		d.id, d.enabled, d.kind = r.ID, r.Enabled, r.Kind
		d.keywords, d.exclude, d.author, d.star, d.read = r.Keywords, r.Exclude, r.Author, r.Star, r.MarkRead
		d.sources = slices.Clone(r.SourceIDs)
	}
	s.rules.dialog = d
}

func (s *Screen) closeRule() {
	d := &s.rules.dialog
	run := d.run + 1
	*d = ruleForm{run: run}
}

// ruleError is the message of a rule the server refused, and the line it belongs under.
func (s *Screen) ruleError(d *ruleForm, err error) {
	switch code(err) {
	case "rule_no_condition":
		d.condErr = s.text.ErrNoCondition
	case "rule_no_action":
		d.actErr = s.text.ErrNoAction
	case "invalid_rule_keywords":
		d.keysErr = s.text.ErrKeywords
	case "invalid_rule_author":
		d.authErr = s.text.ErrAuthor
	case "invalid_rule_source":
		d.formErr = s.text.ErrSource
	case "too_many_rule_sources":
		d.formErr = s.text.ErrSources
	case "too_many_rules":
		d.formErr = s.text.ErrTooMany
	default:
		d.formErr = s.text.SaveFailed
	}
}

func (s *Screen) saveRule() {
	p := &s.rules
	d := &p.dialog
	if d.saving || s.api == nil {
		return
	}
	d.condErr, d.actErr, d.keysErr, d.authErr, d.formErr = "", "", "", "", ""
	rule := client.NewRule{SourceIDs: slices.Clone(d.sources), Kind: d.kind, Keywords: cleanLines(d.keywords),
		Exclude: cleanLines(d.exclude), Author: strings.TrimSpace(d.author), Star: d.star, MarkRead: d.read, Enabled: d.enabled}
	if len(rule.SourceIDs) == 0 && rule.Kind == "" && rule.Keywords == "" && rule.Author == "" {
		d.condErr = s.text.ErrNoCondition
	}
	if !rule.Star && !rule.MarkRead {
		d.actErr = s.text.ErrNoAction
	}
	if d.condErr != "" || d.actErr != "" {
		return
	}
	d.saving = true
	d.run++
	gen, run, id, api := s.gen, d.run, d.id, s.api
	s.deps.Go(func() func() {
		var saved client.Rule
		var err error
		if id == 0 {
			saved, err = api.CreateRule(context.Background(), rule)
		} else {
			saved, err = api.UpdateRule(context.Background(), id, rule)
		}
		return func() {
			if gen != s.gen || s.expired(err) {
				return
			}
			// The dialog may have been closed, or reopened, while the save was out; only its own state is dropped.
			mine := run == p.dialog.run
			if mine {
				p.dialog.saving = false
			}
			if err != nil {
				if mine {
					s.ruleError(&p.dialog, err)
				}
				return
			}
			if i := p.ruleIndex(saved.ID); i >= 0 {
				p.list[i] = saved
			} else {
				p.list = append(p.list, saved)
			}
			if mine {
				s.closeRule()
			}
		}
	})
}

// ruleDialog draws the dialog that adds or edits a rule.
func (s *Screen) ruleDialog(c *ui.Context) {
	d := &s.rules.dialog
	if !d.open {
		return
	}
	t := c.Theme()
	_, height := c.Size()
	title := s.text.RuleAddTitle
	if d.id != 0 {
		title = s.text.RuleEditTitle
	}
	open := true
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		theme.Scrim(c, backdrop).Padding(theme.Px(t, tokens.Space4))
		panel.Column().Width(theme.Px(t, tokens.DialogWide)).MaxHeight(height - theme.Px(t, tokens.Space8)).Clip().
			Label(title)
		theme.Sheet(c, panel, theme.Dialog)
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(theme.Px(t, tokens.Space3)).
			Padding(theme.Px(t, tokens.Space6), theme.Px(t, tokens.Space6), theme.Px(t, tokens.Space2)).Children(func() {
			chip(c, icons.ListFilter, false)
			ui.Text(c, title).FontSize(theme.Px(t, tokens.TextTitle.Size.En)).FontWeight(tokens.TextTitle.Weight).Grow(1).
				MinWidth(0).Role(ui.RoleHeading).Level(2)
			x, color := theme.ToolButton(c, theme.ToolState{})
			x.AlignSelf(ui.Start).Label(s.text.RuleClose).Tooltip(s.text.RuleClose)
			x.Children(func() { ui.Icon(c, icons.X).FontSize(theme.IconMedium(t)).TextColor(color) })
			if x.Clicked() {
				open = false
			}
		})
		line := t.Border
		if d.scroll.Y <= 0 {
			line = line.Alpha(0)
		}
		ui.Box(c).FillWidth().Height(theme.Px(t, tokens.Hairline)).Background(line)
		ui.Scroll(c).TrackScroll(&d.scroll).FillWidth().Shrink(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Gap(theme.Px(t, tokens.Space4)).
				Padding(theme.Px(t, tokens.Space2), theme.Px(t, tokens.Space6), theme.Px(t, tokens.Space6)).
				Children(func() { s.ruleFields(c, d) })
		})
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(theme.Px(t, tokens.Space2)).
			Padding(theme.Px(t, tokens.Space4), theme.Px(t, tokens.Space6)).Background(theme.Surface(t)).Children(func() {
			ui.Box(c).Grow(1)
			cancel := theme.Button{Style: theme.Secondary, Text: s.text.Cancel}.Draw(c)
			if cancel.Clicked() {
				open = false
			}
			save := theme.Button{Style: theme.Primary, Disabled: d.saving, Text: s.text.Save, Busy: d.saving}.Draw(c)
			if save.Clicked() {
				s.saveRule()
			}
		})
	})
	if !open {
		s.closeRule()
	}
}

func (s *Screen) ruleFields(c *ui.Context, d *ruleForm) {
	t := c.Theme()
	group := func(text string) {
		ui.Text(c, text).FontSize(theme.Px(t, tokens.TextLabel.Size.En)).FontWeight(tokens.TextLabel.Weight)
	}
	help := func(text string) { meta(c, text) }
	set := func(name, hint string, control func()) {
		ui.Column(c).FillWidth().Gap(theme.Px(t, tokens.Space1_5)).Children(func() {
			ui.Text(c, name).FontSize(theme.Px(t, tokens.TextMeta.Size.En)).TextColor(theme.Text2(t))
			if hint != "" {
				help(hint)
			}
			control()
		})
	}
	group(s.text.RuleWhen)
	help(s.text.RuleWhenHelp)

	set(s.text.RuleSub, "", func() { s.subsPicker(c, d) })
	kinds := make([]choice, len(ruleKinds))
	for i, k := range ruleKinds {
		kinds[i] = choice{value: k, name: s.kindName(k)}
	}
	set(s.text.RuleKind, "", func() {
		if v, changed := s.pickIn(c, d.kind, s.text.RuleKind, kinds, true); changed {
			d.kind, d.condErr = v, ""
		}
	})
	lines := func(key, label string, text *string, bad, focus bool) bool {
		before := *text
		area := ui.TextArea(c.Key(key), text).FillWidth().Lines(2, 6).Font("monospace").
			FontSize(theme.Px(t, tokens.TextCode.Size.En)).Label(label).Disabled(d.saving)
		well(c, area, area.Focused(), bad)
		if bad && d.keysErr != "" {
			area.Error(d.keysErr)
		}
		if focus {
			area.Focus()
		}
		return *text != before
	}
	set(s.text.RuleKeywords, s.text.RuleKeywordsHelp, func() {
		if lines("rulekeys", s.text.RuleKeywords, &d.keywords, d.keysErr != "" || d.condErr != "", d.focus) {
			d.keysErr, d.condErr = "", ""
		}
		d.focus = false
		statusLine(c, status{text: d.keysErr})
	})
	set(s.text.RuleExclude, s.text.RuleExcludeHelp, func() {
		if lines("ruleexclude", s.text.RuleExclude, &d.exclude, d.keysErr != "", false) {
			d.keysErr = ""
		}
	})
	set(s.text.RuleAuthor, "", func() {
		before := d.author
		field(c, "ruleauthor", &d.author, 0, d.authErr != "" || d.condErr != "", func(in ui.Element) {
			in.Label(s.text.RuleAuthor).Placeholder(s.text.RuleAuthorHint).Disabled(d.saving)
		})
		if d.author != before {
			d.authErr, d.condErr = "", ""
		}
		statusLine(c, status{text: d.authErr})
		statusLine(c, status{text: d.condErr})
	})
	s.watchRuleCount(c, d)
	if _, key := ruleConditions(d); d.counted && d.asked == key && key != "" {
		line := s.text.RuleCountNone
		if d.count > 0 {
			line = locale.Count(s.text.RuleCount, "n", d.count)
		}
		meta(c, line).FontFeatures("tnum").Role(ui.RoleStatus)
	}

	group(s.text.RuleThen)
	ui.Column(c).FillWidth().Gap(theme.Px(t, tokens.Space2)).Children(func() {
		if ui.Checkbox(c, &d.star, s.text.ActStar).Disabled(d.saving).Changed() {
			d.actErr = ""
		}
		if ui.Checkbox(c, &d.read, s.text.ActRead).Disabled(d.saving).Changed() {
			d.actErr = ""
		}
		statusLine(c, status{text: d.actErr})
	})
	meta(c, s.text.RuleNote)
	if d.formErr != "" {
		ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(theme.Px(t, tokens.Space1_5)).Role(ui.RoleStatus).
			Children(func() { statusText(c, status{text: d.formErr}) })
	}
}

// deleteRule takes a rule away at once and asks the server to; if that fails the rule comes back.
func (s *Screen) deleteRule(id int64) {
	p := &s.rules
	i := p.ruleIndex(id)
	if i < 0 || s.api == nil {
		return
	}
	rule := p.list[i]
	p.list = slices.Delete(slices.Clone(p.list), i, i+1)
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		err := api.DeleteRule(context.Background(), id)
		return func() {
			if gen != s.gen || s.expired(err) {
				return
			}
			if err != nil && code(err) != "not_found" {
				at := min(i, len(p.list))
				p.list = slices.Insert(p.list, at, rule)
				s.toast = s.text.SaveFailed
				return
			}
			p.deleted = &deletion{rule: rule}
		}
	})
}

// deletedToast shows the toast of a deleted rule, with Undo.
func (s *Screen) deletedToast(c *ui.Context) {
	d := s.rules.deleted
	if d == nil || d.shown {
		return
	}
	d.shown = true
	c.AddToast(ui.Toast{ID: deletedToast, Title: s.text.RuleDeleted, Timeout: 8 * time.Second,
		Action: s.text.Undo, OnAction: func() { s.undoDelete(d) },
		OnClose: func() {
			if s.rules.deleted == d {
				s.rules.deleted = nil
			}
		},
		Data: theme.ToastLook{Icon: icons.Trash2, Close: s.text.Dismiss}})
}

// undoDelete adds the rule back in its old place. While signed out it does nothing and keeps the rule, so that
// Undo works once the user is signed in again within the toast's time.
func (s *Screen) undoDelete(d *deletion) {
	p := &s.rules
	if p.deleted != d || s.api == nil {
		return
	}
	p.deleted = nil
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		saved, err := api.RestoreRule(context.Background(), d.rule)
		return func() {
			if gen != s.gen || s.expired(err) {
				return
			}
			if err != nil {
				s.toast = s.text.SaveFailed
				return
			}
			p.list = append(p.list, saved)
			slices.SortStableFunc(p.list, func(a, b client.Rule) int {
				return cmp.Or(cmp.Compare(a.CreatedAt, b.CreatedAt), cmp.Compare(a.ID, b.ID))
			})
		}
	})
}

// mutedCard is the keywords that hide entries in every subscription.
func (s *Screen) mutedCard(c *ui.Context) {
	t := c.Theme()
	p := &s.rules
	s.watchMuted(c)
	sectionTitle(c, s.text.MutedTitle, gap(t))
	plate(c).Children(func() {
		ui.Column(c).FillWidth().PaddingY(theme.Px(t, tokens.Space3)).Children(func() {
			setting(c, icons.EyeOff, false, s.text.MutedHelp, nil, nil)
			before := p.muted
			area := ui.TextArea(c.Key("muted"), &p.muted).FillWidth().Lines(3, 8).Margin(below(t), 0, 0, 0).
				Font("monospace").FontSize(theme.Px(t, tokens.TextCode.Size.En)).Label(s.text.MutedTitle).Disabled(p.saving)
			well(c, area, area.Focused(), p.invalid)
			if p.invalid {
				area.Error(p.mutedState.text)
			}
			if p.focus {
				area.Focus()
				p.focus = false
			}
			if p.muted != before {
				p.invalid, p.mutedState = false, status{}
			}
			if p.mutedState.text != "" {
				statusLine(c, p.mutedState)
			} else if text := cleanLines(p.muted); text != "" && p.counted && p.asked == text {
				line := s.text.MutedNone
				if p.count > 0 {
					line = locale.Count(s.text.MutedMany, "n", p.count)
				}
				meta(c, line).Margin(theme.Px(t, tokens.Space2), 0, 0, 0).FontFeatures("tnum").Role(ui.RoleStatus)
			}
			dirty := cleanLines(p.muted) != cleanLines(s.settings.MuteKeywords)
			style := theme.Secondary
			if dirty {
				style = theme.Primary
			}
			ui.Row(c).FillWidth().AlignItems(ui.Center).Margin(below(t), 0, 0, 0).Children(func() {
				ui.Box(c).Grow(1)
				save := theme.Button{Style: style, Disabled: !dirty || p.saving, Text: s.text.Save, Busy: p.saving}.Draw(c)
				if save.Clicked() {
					s.saveMuted()
				}
			})
		})
	})
}

// watchMuted counts the entries the typed keywords would hide once they have rested for a moment.
func (s *Screen) watchMuted(c *ui.Context) {
	p := &s.rules
	if s.api == nil {
		return
	}
	text := cleanLines(p.muted)
	if text != p.seen {
		p.seen, p.seenAt = text, s.deps.Now()
	}
	if text == "" || text == p.asked {
		return
	}
	if wait := mutedWait - s.deps.Now().Sub(p.seenAt); wait > 0 {
		c.After(wait)
		return
	}
	p.asked, p.counted, p.counting = text, false, true
	p.countRun++
	gen, run, api := s.gen, p.countRun, s.api
	s.deps.Go(func() func() {
		n, err := api.MutedCountAll(context.Background(), text)
		return func() {
			if gen != s.gen || run != p.countRun || s.expired(err) {
				return
			}
			p.counting = false
			if err == nil {
				p.count, p.counted = n, true
			}
		}
	})
}

func (s *Screen) saveMuted() {
	p := &s.rules
	if p.saving || s.api == nil {
		return
	}
	text := cleanLines(p.muted)
	p.saving, p.invalid, p.mutedState = true, false, status{}
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		saved, err := api.SetSettings(context.Background(), client.SettingsPatch{MuteKeywords: &text})
		return func() {
			if gen != s.gen || s.expired(err) {
				return
			}
			p.saving = false
			switch {
			case code(err) == "invalid_mute_keywords":
				p.invalid, p.focus = true, true
				p.mutedState = status{text: s.text.MutedInvalid}
			case err != nil:
				p.mutedState = status{text: s.text.SaveFailed}
			default:
				s.settings = saved
				p.muted, p.mutedState = saved.MuteKeywords, status{ok: true, text: s.text.Saved}
				s.deps.Imported()
			}
		}
	})
}

// ruleConditions is what the dialog asks about: the conditions as the server would see them, and a key that is the
// same for the same conditions. The key is empty when nothing but the exclude list is filled in, which is no condition.
func ruleConditions(d *ruleForm) (client.RuleConditions, string) {
	cond := client.RuleConditions{SourceIDs: slices.Clone(d.sources), Kind: d.kind, Keywords: cleanLines(d.keywords),
		Exclude: cleanLines(d.exclude), Author: strings.TrimSpace(d.author)}
	if len(cond.SourceIDs) == 0 && cond.Kind == "" && cond.Keywords == "" && cond.Author == "" {
		return cond, ""
	}
	slices.Sort(cond.SourceIDs)
	ids := make([]string, len(cond.SourceIDs))
	for i, id := range cond.SourceIDs {
		ids[i] = strconv.FormatInt(id, 10)
	}
	return cond, strings.Join([]string{strings.Join(ids, ","), cond.Kind, cond.Keywords, cond.Exclude, cond.Author}, "\x00")
}

// watchRuleCount counts the existing entries the conditions in the dialog match once they have rested for a moment.
func (s *Screen) watchRuleCount(c *ui.Context, d *ruleForm) {
	if s.api == nil {
		return
	}
	cond, key := ruleConditions(d)
	if key != d.seen {
		d.seen, d.seenAt = key, s.deps.Now()
	}
	if key == "" || key == d.asked {
		return
	}
	if wait := mutedWait - s.deps.Now().Sub(d.seenAt); wait > 0 {
		c.After(wait)
		return
	}
	d.asked, d.counted = key, false
	d.countRun++
	gen, run, api, dialog := s.gen, d.countRun, s.api, d.run
	s.deps.Go(func() func() {
		n, err := api.RuleMatchCount(context.Background(), cond)
		return func() {
			p := &s.rules.dialog
			if gen != s.gen || dialog != p.run || run != p.countRun || s.expired(err) {
				return
			}
			if err == nil {
				p.count, p.counted = n, true
			}
		}
	})
}

// subsPicker is the button that lists the subscriptions a rule can be limited to, with a check box for each.
func (s *Screen) subsPicker(c *ui.Context, d *ruleForm) {
	t := c.Theme()
	label := s.sourceNames(d.sources)
	if len(d.sources) > 1 {
		label = locale.Count(s.text.SubsN, "n", len(d.sources))
	}
	wasOpen := d.subsOpen
	btn := selectTrigger(t, ui.ButtonBase(c.Key("rulesubs")).FillWidth()).AlignItems(ui.Center).
		Label(s.text.RuleSub + ": " + label)
	trigger(c, btn)
	btn.Disabled(d.saving)
	btn.Children(func() {
		ui.Text(c, label).Grow(1).MinWidth(0).SingleLine()
		ui.Icon(c, icons.ChevronDown).FontSize(theme.IconSmall(t)).TextColor(t.TextMuted)
	})
	if btn.Clicked() {
		d.subsOpen = !wasOpen
	}
	ui.PopoverBase(c, btn, &d.subsOpen, func(panel ui.Element) {
		menu(c, panel)
		panel.MinWidth(theme.Px(t, tokens.SheetWidth) * 3 / 4)
		ui.Scroll(c).MaxHeight(theme.Px(t, tokens.SheetWidth)).Children(func() {
			ui.Column(c).FillWidth().Children(func() {
				all := len(d.sources) == 0
				if ui.Checkbox(c.Key("rulesub-all"), &all, s.text.AllSubs).FillWidth().Height(theme.Px(t, tokens.Row)).
					PaddingX(theme.Px(t, tokens.Space2)).Changed() {
					d.sources, d.condErr, d.formErr = nil, "", ""
				}
				ui.Box(c).Key("rulesub-line").FillWidth().Height(theme.Px(t, tokens.Hairline)).
					Margin(theme.Px(t, tokens.Space1), 0).Background(t.Border)
				for _, src := range s.rules.sources {
					on := slices.Contains(d.sources, src.ID)
					row := ui.Checkbox(c.Key("rulesub"+strconv.FormatInt(src.ID, 10)), &on, src.Title).FillWidth().
						Height(theme.Px(t, tokens.Row)).PaddingX(theme.Px(t, tokens.Space2))
					if !row.Changed() {
						continue
					}
					d.condErr, d.formErr = "", ""
					switch {
					case !on:
						d.sources = slices.DeleteFunc(slices.Clone(d.sources), func(id int64) bool { return id == src.ID })
					case len(d.sources) >= maxRuleSources:
						d.formErr = s.text.ErrSources
					default:
						d.sources = append(slices.Clone(d.sources), src.ID)
					}
				}
			})
		})
	})
}
