package settings

import (
	"context"
	"math"
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

// watchHealth reads the health of the RSSHub instances once when the Fetching group is shown.
func (s *Screen) watchHealth() {
	if !s.healthAsked {
		s.fetchHealth()
	}
}

// fetchHealth reads the health of the RSSHub instances in the background. A result a newer request has passed is
// dropped. A failed read leaves the figures out, with no error and no retry: they are a hint beside the setting.
func (s *Screen) fetchHealth() {
	if s.api == nil {
		return
	}
	s.healthAsked = true
	s.healthLoads++
	gen, run, api := s.gen, s.healthLoads, s.api
	s.deps.Go(func() func() {
		health, err := api.RSSHubHealth(context.Background())
		return func() {
			if gen != s.gen || run != s.healthLoads || s.expired(err) {
				return
			}
			s.healthShown = err == nil
			s.health = health
		}
	})
}

// instancesRow is the row of the Fetching group that opens the page of the RSSHub instances.
func (s *Screen) instancesRow(c *ui.Context) {
	t := c.Theme()
	count := s.fill(s.text.InstanceCount, "{n}", strconv.Itoa(len(s.settings.RSSHubBaseURLs)))
	row := ui.ButtonBase(c).Row().FillWidth().AlignItems(ui.Center).Gap(theme.Px(t, tokens.Space3)).
		Radius(theme.Px(t, tokens.RadiusControl)).Label(s.text.RSSHub)
	row.Children(func() {
		chip(c, icons.Rss, false)
		ui.Text(c, s.text.RSSHub).Grow(1).Shrink(1).MinWidth(0).Wrap()
		ui.Text(c, count).FontFeatures("tnum").TextColor(t.TextMuted).NoWrap()
		ui.Icon(c, icons.ChevronRight).FontSize(theme.IconSmall(t)).TextColor(t.TextMuted)
	})
	if row.Clicked() {
		s.instances, s.baseStatus = true, status{}
	}
}

// instancesPage lists the RSSHub instances in order, each with how its latest requests went, over the row that adds one.
func (s *Screen) instancesPage(c *ui.Context) {
	sectionHead(c, s.text.Instances, s.text.RSSHubHelp, 0)
	list := s.settings.RSSHubBaseURLs
	rows := make([]cardRow, 0, len(list)+1)
	for i, address := range list {
		rows = append(rows, cardRow{key: address, draw: func() { s.instanceRow(c, i, address) }})
	}
	rows = append(rows, cardRow{key: "add", draw: func() { s.addInstanceRow(c) }})
	cardOf(c, rows)
}

// addInstanceRow is the field that adds an instance; Enter adds what is typed.
func (s *Screen) addInstanceRow(c *ui.Context) {
	t := c.Theme()
	before := s.adding
	labeled(c, icons.Plus, s.text.AddInstance, s.text.InstanceLimit, func() {
		s.watch(field(c, "base", &s.adding, theme.Px(t, tokens.SheetWidth), s.invalid, func(in ui.Element) {
			in.Label(s.text.AddInstance).Placeholder(s.text.RSSHubPlaceholder).Disabled(s.saving || len(s.settings.RSSHubBaseURLs) >= maxInstances)
			if s.invalid {
				in.Error(s.baseStatus.text)
			}
			if s.focusBase {
				in.Focus()
				s.focusBase = false
			}
			if in.Submitted() {
				s.addInstance()
			}
		}))
	})
	statusLine(c, s.baseStatus)
	if s.adding != before {
		s.invalid, s.baseStatus = false, status{}
	}
}

// instanceHealth is what the server last said about the saved instance at i; false before it has said anything.
func (s *Screen) instanceHealth(i int) (client.InstanceHealth, bool) {
	if !s.healthShown || !s.healthSaved() {
		return client.InstanceHealth{}, false
	}
	return s.health.Instances[i], true
}

// healthSaved reports whether the health read is of the instances saved, which it is not for the moment between a
// save and the next read.
func (s *Screen) healthSaved() bool {
	if len(s.health.Instances) != len(s.settings.RSSHubBaseURLs) {
		return false
	}
	for i, in := range s.health.Instances {
		if in.URL != s.settings.RSSHubBaseURLs[i] {
			return false
		}
	}
	return true
}

// instanceRow is one instance: its host, how its latest requests went, what it is in the list and its menu.
func (s *Screen) instanceRow(c *ui.Context, i int, address string) {
	t := c.Theme()
	host := hostOf(address)
	health, known := s.instanceHealth(i)
	cooling := known && health.CoolingUntil != nil
	figures := ""
	if known {
		figures = s.figures(health, s.deps.Now())
	}
	setting(c, icons.Server, cooling, host, func() {
		if figures != "" {
			line := meta(c, figures).FontFeatures("tnum")
			if cooling {
				line.TextColor(t.Danger)
			}
		}
	}, func() {
		switch {
		case cooling:
			pill(c, s.text.CoolingTag, theme.Soft(t, t.Danger), t.Danger)
		case i == 0:
			pill(c, s.text.MainBadge, theme.Soft(t, t.Accent), t.Accent)
		default:
			ui.Text(c, s.text.Standby).TextColor(theme.Text2(t)).NoWrap()
		}
		s.instanceMore(c, i, host)
	})
}

// instanceMore is the ellipsis of an instance's row, whose menu orders and removes it.
func (s *Screen) instanceMore(c *ui.Context, i int, host string) {
	t := c.Theme()
	last := len(s.settings.RSSHubBaseURLs) - 1
	more, color := theme.ToolButton(c, theme.ToolState{})
	label := s.fill(s.text.InstanceMore, "{name}", host)
	more.Label(label).Tooltip(label)
	more.Children(func() { ui.Icon(c, icons.Ellipsis).FontSize(theme.IconMedium(t)).TextColor(color) })
	more.Menu(func(m *ui.Menu) {
		if i > 0 && m.Item(s.text.MakeMain).Chosen() {
			s.moveInstance(i, 0)
		}
		if i > 1 && m.Item(s.text.MoveUp).Chosen() {
			s.moveInstance(i, i-1)
		}
		if i < last && m.Item(s.text.MoveDown).Chosen() {
			s.moveInstance(i, i+1)
		}
		if m.Item(s.text.RemoveInstance).Chosen() {
			s.removeInstance(i)
		}
	})
}

// figures says how an instance's latest requests went: how many, the success rate and median latency or that nothing
// answered, and when a cooling instance is tried again.
func (s *Screen) figures(in client.InstanceHealth, now time.Time) string {
	if in.Requests == 0 || in.SuccessRate == nil {
		return s.text.NoData
	}
	parts := []string{s.fill(s.text.LastN, "{n}", strconv.Itoa(in.Requests))}
	if in.MedianMs == nil {
		parts = append(parts, s.text.NoAnswer)
	} else {
		parts = append(parts, s.fill(s.text.HealthRate, "{rate}", percent(*in.SuccessRate)),
			s.fill(s.text.HealthMedian, "{time}", s.latency(*in.MedianMs)))
	}
	if in.CoolingUntil != nil {
		parts = append(parts, s.cooling(*in.CoolingUntil, now))
	}
	return strings.Join(parts, " · ")
}

// cooling says when a cooling instance is tried again, in whole minutes rounded up.
func (s *Screen) cooling(until int64, now time.Time) string {
	left := time.UnixMilli(until).Sub(now)
	if left <= time.Minute {
		return s.text.CoolingSoon
	}
	minutes := int(math.Ceil(left.Minutes()))
	return locale.Count(s.text.Cooling, "n", minutes)
}

// percent is a success rate in whole percent, rounded down so that 100% means every request was answered, and "<1%"
// for a rate above zero that rounds down to nothing.
func percent(rate float64) string {
	whole := int(math.Floor(rate*100 + 1e-9))
	if whole == 0 && rate > 0 {
		return "<1%"
	}
	return strconv.Itoa(whole) + "%"
}

// latency is a duration in milliseconds below a second, and in seconds with one decimal from there.
func (s *Screen) latency(ms int64) string {
	if ms < 1000 {
		return s.fill(s.text.Milliseconds, "{n}", strconv.FormatInt(ms, 10))
	}
	return s.fill(s.text.Seconds, "{n}", strconv.FormatFloat(float64(ms)/1000, 'f', 1, 64))
}

// hostOf is an instance address without its scheme.
func hostOf(address string) string {
	if _, rest, ok := strings.Cut(address, "://"); ok {
		return rest
	}
	return address
}
