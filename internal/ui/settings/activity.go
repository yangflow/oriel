package settings

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/ui/icons"
	"github.com/yangflow/oriel/internal/ui/locale"
	"github.com/yangflow/oriel/internal/ui/theme"
)

// How often the page asks the server again while it is shown, and how often while a compaction runs.
const (
	activityEvery = 5 * time.Second
	vacuumEvery   = 2 * time.Second
)

// compacting is whether the database is being compacted, here or on another device.
func (s *Screen) compacting() bool {
	return s.vacStarting || s.storage.Vacuum.State == client.VacuumRunning
}

// watchActivity asks for the activity again once its time has come, and wakes the screen for the next time. It runs
// only while the page is shown, so the other groups cost the server nothing. A failed page waits for Retry.
func (s *Screen) watchActivity(c *ui.Context) {
	if s.api == nil || s.actBusy || s.actState == failed {
		return
	}
	every := activityEvery
	if s.compacting() {
		every = vacuumEvery
	}
	if wait := every - s.deps.Now().Sub(s.actAt); wait > 0 {
		c.After(wait)
		return
	}
	s.fetchActivity()
}

// fetchActivity reads the activity and the storage in the background. A request passed by a newer one is dropped. A
// failed refresh keeps what the page shows; only a page that has nothing yet turns into the failure.
func (s *Screen) fetchActivity() {
	if s.api == nil {
		return
	}
	if s.actState == failed {
		s.actState = loading
	}
	s.actLoads++
	s.actBusy, s.actAt = true, s.deps.Now()
	gen, run, api := s.gen, s.actLoads, s.api
	s.deps.Go(func() func() {
		activity, activityErr := api.Activity(context.Background())
		storage, storageErr := api.Storage(context.Background())
		return func() {
			if gen != s.gen || run != s.actLoads {
				return
			}
			s.actBusy = false
			if s.expired(activityErr) || s.expired(storageErr) {
				return
			}
			if activityErr != nil || storageErr != nil {
				if s.actState != loaded {
					s.actState = failed
					s.announce = s.text.Error
				}
				return
			}
			s.activity, s.storage, s.actState = activity, storage, loaded
		}
	})
}

// startVacuum asks the server to compact the database. The server does it in the background; the page then follows
// the state it reports.
func (s *Screen) startVacuum() {
	if s.api == nil || s.compacting() {
		return
	}
	s.vacStarting, s.vacError = true, ""
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		vacuum, err := api.StartVacuum(context.Background())
		return func() {
			if gen != s.gen {
				return
			}
			s.vacStarting = false
			if s.expired(err) {
				return
			}
			switch {
			case code(err) == "vacuum_running":
				// Another device started it first: follow it, and read the real state at once.
				s.storage.Vacuum.State = client.VacuumRunning
				s.actLoads++
				s.actBusy, s.actAt = false, time.Time{}
			case err != nil:
				reason := code(err)
				if reason == "" {
					reason = err.Error()
				}
				s.vacError = s.fill(s.text.CompactFailed, "{reason}", reason)
			default:
				s.storage.Vacuum = vacuum
				s.actLoads++ // a read that began before the start would bring back the old state
				s.actBusy, s.actAt = false, s.deps.Now()
			}
		}
	})
}

// activityPage is the storage, what is being fetched now and how the recent fetches went.
func (s *Screen) activityPage(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(t.Space(3)).Margin(0, 0, theme.Px(t, 20), 0).Children(func() {
		ui.Text(c, s.text.ActivityLead).TextColor(t.TextMuted).Wrap().Grow(1).Shrink(1).MinWidth(0)
		refresh := theme.Button{Small: true, Icon: icons.RefreshCw, Text: s.text.Refresh}.Draw(c)
		if refresh.Clicked() {
			s.fetchActivity()
		}
	})
	s.storageCard(c)
	s.diagnosticsCard(c)
	s.fetchingNowCard(c)
	s.recentCard(c)
}

func (s *Screen) storageCard(c *ui.Context) {
	t := c.Theme()
	st := s.storage
	sectionTitle(c, s.text.Storage, 0)
	card(c, func() {
		ui.Column(c).FillWidth().Gap(theme.Px(t, 8)).Children(func() {
			s.keyValue(c, s.text.DBSize, s.bytes(st.DatabaseBytes))
			s.keyValue(c, s.text.Entries, strconv.FormatInt(st.Entries, 10))
			s.keyValue(c, s.text.Images, s.fill(locale.Count(s.text.ImagesValue, "n", st.Images), "{size}", s.bytes(st.ImageBytes)))
			s.keyValue(c, s.text.Copies, s.fill(locale.Count(s.text.CopiesValue, "n", st.Copies), "{size}", s.bytes(st.CopyBytes)))
		})
	}, func() {
		busy := s.compacting()
		t := c.Theme()
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(3)).Children(func() {
			label := s.text.Compact
			if busy {
				label = s.text.Compacting
			}
			compact := theme.Button{Disabled: busy, Text: label, Busy: busy}.Draw(c)
			if compact.Clicked() {
				s.startVacuum()
			}
			ui.Text(c, s.fill(s.text.CompactHelp, "{size}", s.bytes(st.ReclaimableBytes))).FontSize(theme.Px(t, 12)).
				TextColor(t.TextMuted).Wrap().Grow(1).Shrink(1).MinWidth(0)
		})
		statusLine(c, s.vacuumStatus())
	})
}

// vacuumStatus is what the last compaction, or the request for one, came to; nothing before the first.
func (s *Screen) vacuumStatus() status {
	v := s.storage.Vacuum
	switch {
	case s.vacError != "":
		return status{warn: true, text: s.vacError}
	case s.compacting():
		return status{spin: true, text: s.text.Compacting}
	case v.State == client.VacuumDone:
		return status{ok: true, text: s.fill(s.text.Compacted, "{size}", s.bytes(v.FreedBytes))}
	case v.State == client.VacuumFailed:
		return status{warn: true, text: s.fill(s.text.CompactFailed, "{reason}", v.Error)}
	}
	return status{}
}

// diagnosticsCard exports the diagnostics file: what it holds, the button and how the last export went.
func (s *Screen) diagnosticsCard(c *ui.Context) {
	t := c.Theme()
	sectionTitle(c, s.text.Diagnostics, t.Space(6))
	card(c, func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(3)).Children(func() {
			label := s.text.ExportDiagnostics
			if s.diagBusy {
				label = s.text.ExportingDiagnostics
			}
			export := theme.Button{Disabled: s.diagBusy, Icon: icons.Download, Text: label, Busy: s.diagBusy}.Draw(c)
			if export.Clicked() {
				s.exportDiagnostics()
			}
			ui.Text(c, s.text.DiagnosticsHelp).FontSize(theme.Px(t, 12)).TextColor(t.TextMuted).Wrap().Grow(1).Shrink(1).MinWidth(0)
		})
		statusLine(c, s.diagStatus)
	})
}

// ExportDiagnostics starts the export from outside the page, the Help menu; the shell shows the page first.
func (s *Screen) ExportDiagnostics() { s.exportDiagnostics() }

// exportDiagnostics asks where to save the file, then has the server write it and saves what it sent.
func (s *Screen) exportDiagnostics() {
	if s.diagBusy || s.api == nil {
		return
	}
	s.diagBusy, s.diagStatus = true, status{}
	gen, api, filter := s.gen, s.api, s.text.DiagnosticsFiles
	s.deps.Go(func() func() {
		path, err := s.deps.Save("Oriel-diagnostics.txt", filter)
		if err != nil || path == "" {
			return func() {
				if gen == s.gen {
					s.diagBusy = false
				}
			}
		}
		name := filepath.Base(path)
		data, err := api.Diagnostics(context.Background())
		written := err == nil && os.WriteFile(path, data, 0o644) == nil
		return func() {
			if gen != s.gen || s.expired(err) {
				return
			}
			s.diagBusy = false
			switch {
			case err != nil:
				s.diagStatus = status{text: s.text.DiagnosticsFailed}
			case !written:
				s.diagStatus = status{text: s.fill(s.text.WriteFailed, "{file}", name)}
			default:
				s.diagStatus = status{ok: true, text: s.fill(s.text.DiagnosticsExported, "{file}", name)}
			}
			s.announce = s.diagStatus.text
		}
	})
}

func (s *Screen) keyValue(c *ui.Context, label, value string) {
	t := c.Theme()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(3)).Children(func() {
		ui.Text(c, label).Grow(1).Shrink(1).MinWidth(0).SingleLine()
		ui.Text(c, value).SingleLine().FontWeight(500)
	})
}

func (s *Screen) fetchingNowCard(c *ui.Context) {
	t := c.Theme()
	sectionTitle(c, s.text.FetchingNow, t.Space(6))
	card(c, func() {
		if len(s.activity.Fetching) == 0 {
			ui.Text(c, s.text.FetchingNone).TextColor(t.TextMuted).Wrap()
			return
		}
		ui.Column(c).FillWidth().Children(func() {
			for _, f := range s.activity.Fetching {
				ui.Row(c).Key(f.ID).FillWidth().Height(theme.Px(t, 36)).AlignItems(ui.Center).Gap(theme.Px(t, 10)).Children(func() {
					spin := ui.Icon(c, icons.LoaderCircle).FontSize(theme.IconSmall(t)).TextColor(t.Accent)
					spin.Rotate(spin.Loop("spin", 900*time.Millisecond, ui.Linear) * 360)
					ui.Text(c, sourceName(f.Title, f.RSSHubPath, f.URL)).Grow(1).Shrink(1).MinWidth(0).SingleLine()
				})
			}
		})
	})
}

func (s *Screen) recentCard(c *ui.Context) {
	t := c.Theme()
	sectionTitle(c, s.text.Recent, t.Space(6))
	if len(s.activity.Sources) == 0 {
		card(c, func() { ui.Text(c, s.text.RecentNone).TextColor(t.TextMuted).Wrap() })
		return
	}
	now := s.deps.Now()
	ui.Column(c).FillWidth().Radius(12).Background(t.Surface).Children(func() {
		for i, src := range s.activity.Sources {
			if i > 0 {
				ui.Box(c).FillWidth().Height(1).Margin(0, theme.Px(t, 20)).Background(t.Border)
			}
			s.sourceRow(c, src, now)
		}
	})
	ui.Text(c, locale.Count(s.text.RecentKeep, "n", s.activity.Keep)).FontSize(theme.Px(t, 12)).
		TextColor(t.TextMuted).Wrap().Margin(theme.Px(t, 8), theme.Px(t, 4), 0, theme.Px(t, 4))
}

// sourceRow is a subscription with its last fetch, which opens the list of its recent fetches. Only the open row draws
// them.
func (s *Screen) sourceRow(c *ui.Context, src client.ActivitySource, now time.Time) {
	t := c.Theme()
	open := s.actOpen == src.ID
	name := sourceName(src.Title, src.RSSHubPath, src.URL)
	ui.Column(c).Key(src.ID).FillWidth().Children(func() {
		row := ui.ButtonBase(c).FillWidth().Height(theme.Px(t, 44)).AlignItems(ui.Center).Gap(theme.Px(t, 10)).PaddingX(theme.Px(t, 20)).
			Label(s.fill(s.text.ShowAttempts, "{name}", name)).Role(ui.RoleDisclosure).Expanded(open)
		switch {
		case row.Pressed():
			row.Background(t.SurfacePressed)
		case row.Hovered():
			row.Background(t.SurfaceHover)
		}
		theme.Fade(row)
		row.Children(func() {
			summary := ""
			ok := true
			if len(src.Attempts) > 0 {
				last := src.Attempts[0]
				ok = last.OK
				summary = s.ago(last.At, now) + " · " + s.outcome(last)
			}
			s.resultIcon(c, ok)
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				ui.Text(c, name).FontWeight(500).SingleLine().MinWidth(0)
				ui.Text(c, summary).FontSize(theme.Px(t, 12)).TextColor(t.TextMuted).SingleLine().MinWidth(0)
			})
			chevron := icons.ChevronRight
			if open {
				chevron = icons.ChevronDown
			}
			ui.Icon(c, chevron).FontSize(theme.IconSmall(t)).TextColor(t.TextMuted)
		})
		if row.Clicked() {
			if open {
				s.actOpen = 0
			} else {
				s.actOpen = src.ID
			}
		}
		if open {
			ui.Column(c).FillWidth().Padding(0, theme.Px(t, 20), theme.Px(t, 8), theme.Px(t, 20)).Children(func() {
				for i, a := range src.Attempts {
					s.attemptLine(c, i, a, now)
				}
			})
		}
	})
}

// attemptLine is one fetch: how it went, when, the HTTP status, how long it took and what it brought; a failed one
// says why on a second line.
func (s *Screen) attemptLine(c *ui.Context, i int, a client.FetchAttempt, now time.Time) {
	t := c.Theme()
	ui.Column(c).Key(i).FillWidth().Children(func() {
		ui.Row(c).FillWidth().MinHeight(theme.Px(t, 28)).AlignItems(ui.Center).Gap(theme.Px(t, 10)).Children(func() {
			s.resultIcon(c, a.OK)
			ui.Text(c, s.ago(a.At, now)).Grow(1).Shrink(1).MinWidth(0).SingleLine()
			muted := ui.Text(c, s.status(a)).SingleLine().FontSize(theme.Px(t, 12)).TextColor(t.TextMuted)
			muted.Width(theme.Px(t, 40))
			ui.Text(c, s.duration(a.DurationMs)).SingleLine().FontSize(theme.Px(t, 12)).TextColor(t.TextMuted).Width(theme.Px(t, 56))
			ui.Text(c, s.outcome(a)).SingleLine().FontSize(theme.Px(t, 12)).Width(theme.Px(t, 84))
		})
		if !a.OK && a.Error != "" {
			ui.Text(c, a.Error).FontSize(theme.Px(t, 12)).TextColor(t.TextMuted).Wrap().Margin(0, 0, theme.Px(t, 4), theme.Px(t, 24))
		}
	})
}

func (s *Screen) resultIcon(c *ui.Context, ok bool) {
	t := c.Theme()
	if ok {
		ui.Icon(c, icons.Check).FontSize(theme.IconSmall(t)).TextColor(t.Success)
		return
	}
	ui.Icon(c, icons.CircleAlert).FontSize(theme.IconSmall(t)).TextColor(t.Warning)
}

// outcome is what a fetch came to: how many new entries, nothing new, or that it failed.
func (s *Screen) outcome(a client.FetchAttempt) string {
	switch {
	case !a.OK:
		return s.text.FailedWord
	case a.NewEntries == 0:
		return s.text.NothingNew
	}
	return s.fill(s.text.NewCount, "{n}", strconv.Itoa(a.NewEntries))
}

// status is the HTTP status of a fetch, a dash for one that never got an answer.
func (s *Screen) status(a client.FetchAttempt) string {
	if a.Status == 0 {
		return "–"
	}
	return strconv.Itoa(a.Status)
}

// duration is how long a fetch took.
func (s *Screen) duration(ms int64) string {
	if ms < 1000 {
		return strconv.FormatInt(ms, 10) + " ms"
	}
	return strconv.FormatFloat(float64(ms)/1000, 'f', 1, 64) + " s"
}

// bytes is a size in the largest unit that keeps it above one, with a decimal from megabytes up.
func (s *Screen) bytes(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	v, suffix := float64(n)/unit, "KB"
	for _, next := range []string{"MB", "GB", "TB"} {
		if v < unit {
			break
		}
		v, suffix = v/unit, next
	}
	digits := 1
	if suffix == "KB" {
		digits = 0
	}
	return strconv.FormatFloat(v, 'f', digits, 64) + " " + suffix
}

// sourceName is how a subscription is called: its title, else its route, else its address.
func sourceName(title, route, url string) string {
	switch {
	case title != "":
		return title
	case route != "":
		return route
	}
	return url
}
