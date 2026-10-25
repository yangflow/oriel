package settings

import (
	"errors"
	"reflect"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
)

// openRules opens the Rules group over the given rules and subscriptions.
func openRules(t *testing.T, language string, rules []client.Rule) (*Screen, *fake, *ui.Tester) {
	t.Helper()
	s, f, tt := setup(t, language, true)
	f.rules = rules
	f.sources = []client.Source{{ID: 5, Title: "Go Blog"}, {ID: 2, Title: "Harbor"}}
	s.OpenPage("rules")
	tt.Frame()
	f.settle(tt)
	return s, f, tt
}

func oneRule() []client.Rule {
	return []client.Rule{{ID: 1, SourceIDs: []int64{5}, Keywords: "release\nbeta", Star: true, Enabled: true}}
}

func TestRulesList(t *testing.T) {
	s, _, tt := openRules(t, "en", append(oneRule(),
		client.Rule{ID: 2, Author: "Ann", MarkRead: true, Kind: "video"}))
	for _, text := range []string{s.text.RulesLead, "Title or summary has “release”, “beta”", "Go Blog",
		"By “Ann”", s.text.ActStar, s.text.ActRead, s.text.RuleOffBadge, s.text.AddRule, s.text.MutedTitle} {
		if !tt.HasText(text) {
			t.Errorf("no %q in %v", text, tt.Texts())
		}
	}
	if tt.HasText(s.text.RulesNone) {
		t.Error("the empty state shows over rules")
	}
}

func TestRulesEmpty(t *testing.T) {
	s, _, tt := openRules(t, "en", nil)
	if !tt.HasText(s.text.RulesNone) || !tt.HasText(s.text.AddRule) {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestRulesToggle(t *testing.T) {
	s, f, tt := openRules(t, "en", oneRule())
	press(t, tt, s.text.RuleSwitch)
	f.settle(tt)
	if f.rules[0].Enabled || s.rules.list[0].Enabled || !tt.HasText(s.text.RuleOffBadge) {
		t.Fatalf("server %v, screen %v", f.rules[0].Enabled, s.rules.list[0].Enabled)
	}
	f.ruleErr = errors.New("down")
	press(t, tt, s.text.RuleSwitch)
	f.settle(tt)
	if s.rules.list[0].Enabled || !tt.HasText(s.text.SaveFailed) {
		t.Fatalf("on %v, texts %v", s.rules.list[0].Enabled, tt.Texts())
	}
}

func TestRulesDeleteUndo(t *testing.T) {
	s, f, tt := openRules(t, "en", oneRule())
	press(t, tt, s.text.RuleMore)
	if err := tt.ChooseMenuItem(s.text.MenuDelete); err != nil {
		t.Fatalf("%v in %v", err, tt.Menu())
	}
	tt.Frame()
	if len(s.rules.list) != 0 {
		t.Fatal("the rule is still listed before the server answers")
	}
	f.settle(tt)
	if !reflect.DeepEqual(f.deletedIDs, []int64{1}) || !tt.HasText(s.text.RuleDeleted) || !tt.HasText(s.text.RulesNone) {
		t.Fatalf("deleted %v, texts %v", f.deletedIDs, tt.Texts())
	}
	press(t, tt, s.text.Undo)
	f.settle(tt)
	if len(f.rules) != 1 || len(s.rules.list) != 1 || s.rules.list[0].Keywords != "release\nbeta" ||
		!s.rules.list[0].Star || !reflect.DeepEqual(s.rules.list[0].SourceIDs, []int64{5}) {
		t.Fatalf("server %v, screen %v", f.rules, s.rules.list)
	}
	if tt.HasText(s.text.RuleDeleted) {
		t.Error("the toast stays after Undo")
	}
}

// threeRules are rules in the order the server lists them, the first of which the tests delete.
func threeRules() []client.Rule {
	return []client.Rule{
		{ID: 1, Keywords: "alpha", Star: true, Enabled: true, CreatedAt: 1000},
		{ID: 2, Keywords: "beta", Star: true, Enabled: true, CreatedAt: 2000},
		{ID: 3, Keywords: "gamma", Star: true, Enabled: true, CreatedAt: 3000},
	}
}

func deleteFirst(t *testing.T, s *Screen, f *fake, tt *ui.Tester) {
	t.Helper()
	press(t, tt, s.text.RuleMore)
	if err := tt.ChooseMenuItem(s.text.MenuDelete); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if !reflect.DeepEqual(f.deletedIDs, []int64{1}) || !tt.HasText(s.text.RuleDeleted) {
		t.Fatalf("deleted %v, texts %v", f.deletedIDs, tt.Texts())
	}
}

func ruleIDs(rules []client.Rule) []int64 {
	var ids []int64
	for _, r := range rules {
		ids = append(ids, r.ID)
	}
	return ids
}

// TestRulesUndoKeepsThePlace has Undo put a rule back where it was, with its old ID and creation time.
func TestRulesUndoKeepsThePlace(t *testing.T) {
	s, f, tt := openRules(t, "en", threeRules())
	deleteFirst(t, s, f, tt)
	press(t, tt, s.text.Undo)
	f.settle(tt)
	if len(f.ruleBack) != 1 || f.ruleBack[0].ID != 1 || f.ruleBack[0].CreatedAt != 1000 || len(f.ruleSent) != 0 {
		t.Fatalf("restored %+v, created %+v", f.ruleBack, f.ruleSent)
	}
	if want := []int64{1, 2, 3}; !reflect.DeepEqual(ruleIDs(s.rules.list), want) || !reflect.DeepEqual(ruleIDs(f.rules), want) {
		t.Fatalf("screen %v, server %v", ruleIDs(s.rules.list), ruleIDs(f.rules))
	}
}

// TestRulesUndoAfterReset has the Undo of a deleted rule outlive the page being reset by an expired session and a new
// sign-in on the same server.
func TestRulesUndoAfterReset(t *testing.T) {
	s, f, tt := openRules(t, "en", threeRules())
	deleteFirst(t, s, f, tt)
	if !s.expired(client.ErrUnauthorized) || s.rules.deleted == nil {
		t.Fatal("the deleted rule is dropped by the reset")
	}
	s.Use(f, "reader.example.com", true)
	s.OpenPage("rules")
	tt.Frame()
	f.settle(tt)
	if !tt.HasText(s.text.RuleDeleted) {
		t.Fatalf("the toast went: %v", tt.Texts())
	}
	press(t, tt, s.text.Undo)
	f.settle(tt)
	if len(f.ruleBack) != 1 || !reflect.DeepEqual(ruleIDs(f.rules), []int64{1, 2, 3}) {
		t.Fatalf("restored %+v, server %v", f.ruleBack, ruleIDs(f.rules))
	}
	if got := ruleIDs(s.rules.list); !reflect.DeepEqual(got, []int64{1, 2, 3}) {
		t.Errorf("screen %v", got)
	}
}

func TestRulesDeleteFails(t *testing.T) {
	s, f, tt := openRules(t, "en", oneRule())
	f.ruleErr = errors.New("down")
	press(t, tt, s.text.RuleMore)
	if err := tt.ChooseMenuItem(s.text.MenuDelete); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if len(s.rules.list) != 1 || !tt.HasText(s.text.SaveFailed) || tt.HasText(s.text.RuleDeleted) {
		t.Fatalf("list %v, texts %v", s.rules.list, tt.Texts())
	}
}

func TestRulesAdd(t *testing.T) {
	s, f, tt := openRules(t, "en", nil)
	press(t, tt, s.text.AddRule)
	if !s.rules.dialog.open {
		t.Fatalf("texts %v", tt.Texts())
	}
	// Nothing filled in: both lines show and nothing is sent.
	saveDialog(t, tt, s)
	f.settle(tt)
	if !tt.HasText(s.text.ErrNoCondition) || !tt.HasText(s.text.ErrNoAction) || len(f.ruleSent) != 0 {
		t.Fatalf("sent %v, texts %v", f.ruleSent, tt.Texts())
	}
	s.rules.dialog.focus = true
	tt.Frame()
	tt.Type("beta\n  \nrelease ")
	tt.Frame()
	if tt.HasText(s.text.ErrNoCondition) {
		t.Error("the condition line stays after typing")
	}
	press(t, tt, s.text.ActStar)
	if tt.HasText(s.text.ErrNoAction) {
		t.Error("the action line stays after picking one")
	}
	saveDialog(t, tt, s)
	f.settle(tt)
	want := client.NewRule{Keywords: "beta\nrelease", Star: true, Enabled: true}
	if !reflect.DeepEqual(f.ruleSent, []client.NewRule{want}) {
		t.Fatalf("sent %+v", f.ruleSent)
	}
	if s.rules.dialog.open || len(s.rules.list) != 1 || !tt.HasText("Title or summary has “beta”, “release”") {
		t.Fatalf("list %v, texts %v", s.rules.list, tt.Texts())
	}
}

func TestRulesServerErrors(t *testing.T) {
	for code, text := range map[string]func(*Screen) string{
		"rule_no_condition":     func(s *Screen) string { return s.text.ErrNoCondition },
		"rule_no_action":        func(s *Screen) string { return s.text.ErrNoAction },
		"invalid_rule_keywords": func(s *Screen) string { return s.text.ErrKeywords },
		"invalid_rule_author":   func(s *Screen) string { return s.text.ErrAuthor },
		"invalid_rule_source":   func(s *Screen) string { return s.text.ErrSource },
		"too_many_rules":        func(s *Screen) string { return s.text.ErrTooMany },
		"other":                 func(s *Screen) string { return s.text.SaveFailed },
	} {
		t.Run(code, func(t *testing.T) {
			s, f, tt := openRules(t, "en", nil)
			press(t, tt, s.text.AddRule)
			tt.Type("beta")
			press(t, tt, s.text.ActRead)
			f.ruleErr = &client.APIError{Status: 400, Code: code}
			saveDialog(t, tt, s)
			f.settle(tt)
			if !tt.HasText(text(s)) || !s.rules.dialog.open || len(s.rules.list) != 0 {
				t.Fatalf("want %q in %v", text(s), tt.Texts())
			}
		})
	}
}

func TestRulesEdit(t *testing.T) {
	s, f, tt := openRules(t, "en", oneRule())
	press(t, tt, s.text.RuleMore)
	if err := tt.ChooseMenuItem(s.text.MenuEdit); err != nil {
		t.Fatalf("%v in %v", err, tt.Menu())
	}
	tt.Frame()
	if !s.rules.dialog.open {
		t.Fatalf("texts %v", tt.Texts())
	}
	press(t, tt, s.text.ActRead)
	saveDialog(t, tt, s)
	f.settle(tt)
	want := client.NewRule{SourceIDs: []int64{5}, Keywords: "release\nbeta", Star: true, MarkRead: true, Enabled: true}
	if !reflect.DeepEqual(f.ruleSent, []client.NewRule{want}) || len(s.rules.list) != 1 || !s.rules.list[0].MarkRead ||
		s.rules.dialog.open {
		t.Fatalf("sent %+v, list %+v", f.ruleSent, s.rules.list)
	}
}

func TestRulesDialogCancel(t *testing.T) {
	s, f, tt := openRules(t, "en", nil)
	press(t, tt, s.text.AddRule)
	press(t, tt, s.text.Cancel)
	tt.Frame()
	if s.rules.dialog.open || len(f.ruleSent) != 0 {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestRulesLoadFails(t *testing.T) {
	s, f, tt := setup(t, "en", true)
	f.rulesErr = errors.New("down")
	s.OpenPage("rules")
	tt.Frame()
	f.settle(tt)
	if !tt.HasText(s.text.RulesError) {
		t.Fatalf("texts %v", tt.Texts())
	}
	f.rulesErr = nil
	f.rules = oneRule()
	press(t, tt, s.text.Retry)
	f.settle(tt)
	if tt.HasText(s.text.RulesError) || !tt.HasText(s.text.AddRule) {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestMutedCount(t *testing.T) {
	s, f, tt := openRules(t, "en", nil)
	f.mutedCount = 3
	typeMuted(tt, s, "spoiler\n sale ")
	// The count waits for the keywords to rest.
	f.later += mutedWait / 2
	tt.Advance(mutedWait / 2)
	f.settle(tt)
	if len(f.counted) != 0 {
		t.Fatalf("counted %q before the wait", f.counted)
	}
	f.later += mutedWait
	tt.Advance(mutedWait)
	f.settle(tt)
	if !reflect.DeepEqual(f.counted, []string{"spoiler\nsale"}) || !tt.HasText("Hides 3 entries right now") {
		t.Fatalf("counted %q, texts %v", f.counted, tt.Texts())
	}
}

func TestMutedNone(t *testing.T) {
	s, f, tt := openRules(t, "en", nil)
	typeMuted(tt, s, "spoiler")
	f.later += 2 * mutedWait
	tt.Advance(2 * mutedWait)
	f.settle(tt)
	if !tt.HasText(s.text.MutedNone) {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestMutedSave(t *testing.T) {
	s, f, tt := openRules(t, "en", nil)
	typeMuted(tt, s, "spoiler\n sale ")
	press(t, tt, s.text.Save)
	f.settle(tt)
	if got := f.set[len(f.set)-1].MuteKeywords; got == nil || *got != "spoiler\nsale" {
		t.Fatalf("sent %v", got)
	}
	if f.refreshed != 1 || !tt.HasText(s.text.Saved) {
		t.Fatalf("refreshed %d, texts %v", f.refreshed, tt.Texts())
	}
}

func TestMutedInvalid(t *testing.T) {
	s, f, tt := openRules(t, "en", nil)
	f.setErr = &client.APIError{Status: 400, Code: "invalid_mute_keywords"}
	typeMuted(tt, s, "spoiler")
	press(t, tt, s.text.Save)
	f.settle(tt)
	if !tt.HasText(s.text.MutedInvalid) || f.refreshed != 0 {
		t.Fatalf("refreshed %d, texts %v", f.refreshed, tt.Texts())
	}
	tt.Type("s")
	tt.Frame()
	if tt.HasText(s.text.MutedInvalid) {
		t.Error("the error stays after editing")
	}
}

func TestMutedSaveFails(t *testing.T) {
	s, f, tt := openRules(t, "en", nil)
	f.setErr = errors.New("down")
	typeMuted(tt, s, "spoiler")
	press(t, tt, s.text.Save)
	f.settle(tt)
	if !tt.HasText(s.text.SaveFailed) || f.refreshed != 0 {
		t.Fatalf("texts %v", tt.Texts())
	}
}

func TestMutedClean(t *testing.T) {
	if got := cleanLines(" a \n\n b\n"); got != "a\nb" {
		t.Fatalf("clean %q", got)
	}
}

// typeMuted changes the muted keywords as typed, as the field does between frames.
func typeMuted(tt *ui.Tester, s *Screen, text string) {
	s.rules.muted = text
	tt.Frame()
}

// saveDialog presses Save in the dialog, which sits right of Cancel; the muted keywords have a Save of their own.
func saveDialog(t *testing.T, tt *ui.Tester, s *Screen) {
	t.Helper()
	r, ok := tt.Find(s.text.Cancel)
	if !ok {
		t.Fatalf("no %q in %v", s.text.Cancel, tt.Texts())
	}
	tt.ClickAt(r.X+r.W+40, r.Y+r.H/2)
}

// TestRulesSavedAfterClose keeps a rule the server saved although its dialog was closed while the save was out.
func TestRulesSavedAfterClose(t *testing.T) {
	s, f, tt := openRules(t, "en", nil)
	press(t, tt, s.text.AddRule)
	tt.Type("beta")
	press(t, tt, s.text.ActStar)
	saveDialog(t, tt, s)
	if len(f.queue) != 1 {
		t.Fatalf("queued %d", len(f.queue))
	}
	s.closeRule()
	tt.Frame()
	f.settle(tt)
	if len(f.rules) != 1 || len(s.rules.list) != 1 || s.rules.dialog.open {
		t.Fatalf("server %v, screen %v, open %v", f.rules, s.rules.list, s.rules.dialog.open)
	}
}

// The dialog reads top to bottom as one sentence: the conditions, then what happens, with the note right under the
// actions and the keyword hint right under its label.
func TestRulesDialogReadingOrder(t *testing.T) {
	for _, lang := range []string{"en", "zh-Hans"} {
		s, _, tt := openRules(t, lang, nil)
		press(t, tt, s.text.AddRule)
		var ys []float32
		for _, text := range []string{s.text.RuleWhen, s.text.RuleWhenHelp, s.text.RuleSub, s.text.RuleKind,
			s.text.RuleKeywords, s.text.RuleKeywordsHelp, s.text.RuleExclude, s.text.RuleExcludeHelp, s.text.RuleAuthor, s.text.RuleThen, s.text.ActStar,
			s.text.ActRead, s.text.RuleNote} {
			r, ok := tt.Find(text)
			if !ok {
				t.Fatalf("%s: %q is not in the dialog: %v", lang, text, tt.Texts())
			}
			ys = append(ys, r.Y)
		}
		for i := 1; i < len(ys); i++ {
			if ys[i] <= ys[i-1] {
				t.Errorf("%s: line %d sits at %v, not below %v", lang, i, ys[i], ys[i-1])
			}
		}
	}
}

// TestRulesSummaryShowsNewConditions has a row name the words it skips and the subscriptions it is limited to.
func TestRulesSummaryShowsNewConditions(t *testing.T) {
	_, _, tt := openRules(t, "en", []client.Rule{
		{ID: 1, SourceIDs: []int64{5, 2}, Keywords: "release", Exclude: "beta\nrc", Star: true, Enabled: true},
		{ID: 2, SourceIDs: []int64{5, 2, 9}, Exclude: "ad", Author: "Ann", MarkRead: true, Enabled: true},
	})
	for _, text := range []string{"Title or summary has “release” · Not “beta”, “rc”", "Go Blog, Harbor", "3 subscriptions",
		"Not “ad” · By “Ann”"} {
		if !tt.HasText(text) {
			t.Errorf("no %q in %v", text, tt.Texts())
		}
	}
	_, _, zt := openRules(t, "zh-Hans", []client.Rule{{ID: 1, Keywords: "周报", Exclude: "广告\n推广", Star: true, Enabled: true}})
	if text := "标题或摘要含 “周报” · 不含 “广告”、“推广”"; !zt.HasText(text) {
		t.Errorf("no %q in %v", text, zt.Texts())
	}
}

// TestRulesChooseSubscriptions picks several subscriptions in the list, which sends all of them.
func TestRulesChooseSubscriptions(t *testing.T) {
	s, f, tt := openRules(t, "en", nil)
	press(t, tt, s.text.AddRule)
	press(t, tt, s.text.ActStar)
	press(t, tt, s.text.AllSubs)
	if !s.rules.dialog.subsOpen {
		t.Fatalf("the list is not open: %v", tt.Texts())
	}
	press(t, tt, "Harbor")
	press(t, tt, "Go Blog")
	if !reflect.DeepEqual(s.rules.dialog.sources, []int64{2, 5}) {
		t.Fatalf("chosen %v", s.rules.dialog.sources)
	}
	if want := "2 subscriptions"; !tt.HasText(want) {
		t.Errorf("no %q in %v", want, tt.Texts())
	}
	press(t, tt, "Harbor")
	if !reflect.DeepEqual(s.rules.dialog.sources, []int64{5}) {
		t.Fatalf("after clearing one: %v", s.rules.dialog.sources)
	}
	press(t, tt, s.text.AllSubs)
	if len(s.rules.dialog.sources) != 0 {
		t.Fatalf("All subscriptions leaves %v", s.rules.dialog.sources)
	}
	press(t, tt, "Go Blog")
	saveDialog(t, tt, s)
	f.settle(tt)
	if want := (client.NewRule{SourceIDs: []int64{5}, Star: true, Enabled: true}); !reflect.DeepEqual(f.ruleSent, []client.NewRule{want}) {
		t.Fatalf("sent %+v", f.ruleSent)
	}
}

// TestRulesSubscriptionLimit keeps the list at the number of subscriptions one rule takes.
func TestRulesSubscriptionLimit(t *testing.T) {
	s, _, tt := openRules(t, "en", nil)
	press(t, tt, s.text.AddRule)
	for i := range maxRuleSources {
		s.rules.dialog.sources = append(s.rules.dialog.sources, int64(100+i))
	}
	tt.Frame()
	press(t, tt, "50 subscriptions")
	press(t, tt, "Harbor")
	if len(s.rules.dialog.sources) != maxRuleSources || !tt.HasText(s.text.ErrSources) {
		t.Fatalf("%d chosen, texts %v", len(s.rules.dialog.sources), tt.Texts())
	}
}

// TestRulesExcludeAlone is no condition, and with a condition it is sent with the rule.
func TestRulesExcludeAlone(t *testing.T) {
	s, f, tt := openRules(t, "en", nil)
	press(t, tt, s.text.AddRule)
	press(t, tt, s.text.ActStar)
	s.rules.dialog.exclude = "ad\n sale "
	tt.Frame()
	saveDialog(t, tt, s)
	f.settle(tt)
	if !tt.HasText(s.text.ErrNoCondition) || len(f.ruleSent) != 0 || len(f.ruleAsked) != 0 {
		t.Fatalf("sent %v, asked %v, texts %v", f.ruleSent, f.ruleAsked, tt.Texts())
	}
	s.rules.dialog.keywords = "release"
	tt.Frame()
	saveDialog(t, tt, s)
	f.settle(tt)
	want := client.NewRule{Keywords: "release", Exclude: "ad\nsale", Star: true, Enabled: true}
	if !reflect.DeepEqual(f.ruleSent, []client.NewRule{want}) {
		t.Fatalf("sent %+v", f.ruleSent)
	}
}

// TestRulesMatchCount shows how many existing entries match once the conditions have rested, and drops the answer to
// conditions that have changed since.
func TestRulesMatchCount(t *testing.T) {
	s, f, tt := openRules(t, "en", nil)
	f.ruleCount = 12
	press(t, tt, s.text.AddRule)
	tt.Frame()
	if len(f.ruleAsked) != 0 {
		t.Fatalf("asked %v with nothing filled in", f.ruleAsked)
	}
	s.rules.dialog.keywords = "release"
	s.rules.dialog.exclude = "beta"
	tt.Frame()
	f.later += mutedWait / 2
	tt.Advance(mutedWait / 2)
	f.settle(tt)
	if len(f.ruleAsked) != 0 {
		t.Fatalf("asked %v before the wait", f.ruleAsked)
	}
	f.later += mutedWait
	tt.Advance(mutedWait)
	// The conditions change while the question is out.
	s.rules.dialog.keywords = "release\nnotes"
	tt.Frame()
	f.settle(tt)
	if len(f.ruleAsked) != 1 || f.ruleAsked[0].Keywords != "release" || f.ruleAsked[0].Exclude != "beta" {
		t.Fatalf("asked %+v", f.ruleAsked)
	}
	if tt.HasText("12 of your existing entries match the conditions as they are now") {
		t.Fatalf("the answer to old conditions shows: %v", tt.Texts())
	}
	f.ruleCount = 1
	f.later += 2 * mutedWait
	tt.Advance(2 * mutedWait)
	f.settle(tt)
	if want := "1 of your existing entry matches the conditions as they are now"; !tt.HasText(want) {
		t.Fatalf("no %q in %v", want, tt.Texts())
	}
	f.ruleCount = 0
	s.rules.dialog.author = "Ann"
	tt.Frame()
	f.later += 2 * mutedWait
	tt.Advance(2 * mutedWait)
	f.settle(tt)
	if !tt.HasText(s.text.RuleCountNone) {
		t.Fatalf("texts %v", tt.Texts())
	}
}

// TestRulesMatchCountChinese words the count line in Chinese.
func TestRulesMatchCountChinese(t *testing.T) {
	s, f, tt := openRules(t, "zh-Hans", nil)
	f.ruleCount = 3
	press(t, tt, s.text.AddRule)
	s.rules.dialog.keywords = "周报"
	tt.Frame()
	f.later += 2 * mutedWait
	tt.Advance(2 * mutedWait)
	f.settle(tt)
	if want := "按现在的条件，已有条目里有 3 条会命中"; !tt.HasText(want) {
		t.Fatalf("no %q in %v", want, tt.Texts())
	}
}

// TestRulesTooManySubscriptions words the server's refusal of too many subscriptions.
func TestRulesTooManySubscriptions(t *testing.T) {
	s, f, tt := openRules(t, "en", nil)
	press(t, tt, s.text.AddRule)
	tt.Type("beta")
	press(t, tt, s.text.ActRead)
	f.ruleErr = &client.APIError{Status: 400, Code: "too_many_rule_sources"}
	saveDialog(t, tt, s)
	f.settle(tt)
	if !tt.HasText(s.text.ErrSources) {
		t.Fatalf("texts %v", tt.Texts())
	}
}
