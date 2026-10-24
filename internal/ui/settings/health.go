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
)

// watchHealth reads the health of the RSSHub instances once when the Fetching group is shown.
func (s *Screen) watchHealth() {
	if !s.healthAsked {
		s.fetchHealth()
	}
}

// fetchHealth reads the health of the RSSHub instances in the background. A result a newer request has passed is
// dropped. A failed read hides the block, with no error and no retry: it is a hint beside the setting, not the setting.
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

// healthBlock is the success rate and speed of each saved RSSHub instance, under the field of the addresses. It is
// not drawn when there is no instance or the server did not answer.
func (s *Screen) healthBlock(c *ui.Context) {
	if !s.healthShown || len(s.health.Instances) == 0 || !s.healthSaved() {
		return
	}
	t := c.Theme()
	data := false
	for _, in := range s.health.Instances {
		data = data || in.Requests > 0
	}
	ui.Box(c).FillWidth().Height(1).Background(t.Border).Margin(theme.Px(t, 16), 0, theme.Px(t, 14), 0)
	ui.Column(c).Key("health").FillWidth().Role(ui.RoleGroup).Label(s.text.Health).Children(func() {
		ui.Text(c, s.text.Health).FontWeight(500)
		help := s.text.HealthEmpty
		if data {
			help = s.fill(s.text.HealthHelp, "{keep}", strconv.Itoa(s.health.Keep), "{days}", strconv.Itoa(s.health.Days))
		}
		ui.Text(c, help).FontSize(theme.Px(t, 12)).TextColor(t.TextMuted).Wrap().Margin(theme.Px(t, 2), 0, theme.Px(t, 8), 0)
		if !data {
			return
		}
		now := s.deps.Now()
		for i, in := range s.health.Instances {
			s.instanceRow(c, i, in, now)
		}
	})
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

// instanceRow is one instance: its address, a badge on the first, and how its latest requests went.
func (s *Screen) instanceRow(c *ui.Context, i int, in client.InstanceHealth, now time.Time) {
	t := c.Theme()
	ui.Column(c).Key(i).FillWidth().PaddingY(theme.Px(t, 4)).Children(func() {
		ui.Row(c).FillWidth().MinHeight(theme.Px(t, 24)).AlignItems(ui.Center).Gap(theme.Px(t, 12)).Children(func() {
			addr := ui.Row(c).Grow(1).Shrink(1).MinWidth(0).AlignItems(ui.Center).Gap(theme.Px(t, 8))
			addr.Children(func() {
				ui.Text(c, hostOf(in.URL)).Font("monospace").FontSize(theme.Px(t, 12)).SingleLine().MinWidth(0).Shrink(1)
				if i == 0 {
					ui.Box(c).Height(theme.Px(t, 18)).PaddingX(theme.Px(t, 7)).Radius(theme.Pill(theme.Px(t, 18))).Center().
						Background(theme.Soft(t, t.Accent)).Children(func() {
						ui.Text(c, s.text.MainBadge).FontSize(theme.Px(t, 11)).FontWeight(500).TextColor(t.Accent).NoWrap()
					})
				}
			})
			theme.Tip(c, addr, in.URL)
			color := theme.Text2(t)
			if in.Requests == 0 {
				color = t.TextMuted
			}
			ui.Text(c, s.figures(in)).FontSize(theme.Px(t, 12)).FontFeatures("tnum").TextColor(color).NoWrap()
		})
		if in.CoolingUntil != nil {
			ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(theme.Px(t, 6)).Role(ui.RoleStatus).Margin(theme.Px(t, 2), 0, 0, 0).Children(func() {
				ui.Icon(c, icons.CircleAlert).FontSize(theme.IconSmall(t)).TextColor(t.Warning).Margin(1, 0, 0, 0)
				ui.Text(c, s.cooling(*in.CoolingUntil, now)).FontSize(theme.Px(t, 12)).Wrap().Grow(1).MinWidth(0)
			})
		}
	})
}

// figures is the line at the right of an instance: success rate, median latency and how many requests they count.
func (s *Screen) figures(in client.InstanceHealth) string {
	if in.Requests == 0 || in.SuccessRate == nil {
		return s.text.NoData
	}
	speed := s.text.NoAnswer
	if in.MedianMs != nil {
		speed = latency(*in.MedianMs)
	}
	return percent(*in.SuccessRate) + " · " + speed + " · " + s.fill(s.text.LastN, "{n}", strconv.Itoa(in.Requests))
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
func latency(ms int64) string {
	if ms < 1000 {
		return strconv.FormatInt(ms, 10) + " ms"
	}
	return strconv.FormatFloat(float64(ms)/1000, 'f', 1, 64) + " s"
}

// hostOf is an instance address without its scheme.
func hostOf(address string) string {
	if _, rest, ok := strings.Cut(address, "://"); ok {
		return rest
	}
	return address
}
