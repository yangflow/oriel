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
	"github.com/yangflow/oriel/internal/ui/theme/tokens"
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
	s.storageCard(c)
	s.diagnosticsCard(c)
	s.fetchingNowCard(c)
	s.recentCard(c)
	s.dangerCard(c)
}

func (s *Screen) storageCard(c *ui.Context) {
	st := s.storage
	sectionTitle(c, s.text.Storage, 0)
	rows := []cardRow{
		{key: "db", draw: func() { s.keyValue(c, icons.Database, s.text.DBSize, s.bytes(st.DatabaseBytes)) }},
		{key: "entries", draw: func() { s.keyValue(c, icons.Inbox, s.text.Entries, strconv.FormatInt(st.Entries, 10)) }},
		{key: "images", draw: func() { s.imageCacheRow(c) }},
		{key: "copies", draw: func() {
			s.keyValue(c, icons.Layers, s.text.Copies,
				s.fill(locale.Count(s.text.CopiesValue, "n", st.Copies), "{size}", s.bytes(st.CopyBytes)))
		}},
	}
	if st.PreMigrate != nil {
		rows = append(rows, cardRow{key: "premigrate", draw: func() { s.preMigrateRow(c) }})
	}
	rows = append(rows, cardRow{key: "compact", draw: func() {
		busy := s.compacting()
		setting(c, icons.Minimize2, false, s.text.Compact, func() {
			meta(c, s.fill(s.text.CompactHelp, "{size}", s.bytes(st.ReclaimableBytes)))
		}, func() {
			label := s.text.CompactRun
			if busy {
				label = s.text.Compacting
			}
			compact := theme.Button{Disabled: busy, Text: label, Busy: busy}.Draw(c)
			if compact.Clicked() {
				s.startVacuum()
			}
		})
		statusLine(c, s.vacuumStatus())
	}})
	cardOf(c, rows)
}

// imageCacheRow shows how many pictures the server's image cache holds and the button that clears it. Clearing asks
// nothing: the cache refills on demand and the starred copies are not in it.
func (s *Screen) imageCacheRow(c *ui.Context) {
	st := s.storage
	value := s.fill(locale.Count(s.text.ImagesValue, "n", st.Images), "{size}", s.bytes(st.ImageBytes))
	setting(c, icons.Image, false, s.text.Images, func() { meta(c, value).FontFeatures("tnum") }, func() {
		empty := st.Images == 0 && st.ImageBytes == 0
		clear := theme.Button{Small: true, Disabled: s.clearing || empty, Busy: s.clearing, Text: s.text.ClearImages,
			Label: s.text.ClearImagesLabel}.Draw(c)
		if clear.Clicked() {
			s.clearImageCache()
		}
	})
}

// clearImageCache has the server delete its cached pictures, then reads the storage again.
func (s *Screen) clearImageCache() {
	if s.api == nil || s.clearing {
		return
	}
	s.clearing = true
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		err := api.ClearImageCache(context.Background())
		return func() {
			if gen != s.gen {
				return
			}
			s.clearing = false
			if s.expired(err) {
				return
			}
			if err != nil {
				s.toast = s.text.ClearImagesFailed
				return
			}
			s.storage.Images, s.storage.ImageBytes = 0, 0
			s.actLoads++ // a read that began before the clear would bring the old figures back
			s.actBusy, s.actAt = false, time.Time{}
			s.toast = s.text.ImagesCleared
		}
	})
}

// dangerCard is the actions that cannot be undone.
func (s *Screen) dangerCard(c *ui.Context) {
	sectionTitle(c, s.text.Danger, 0)
	rows := []cardRow{{key: "delete-read", draw: func() {
		setting(c, icons.Trash2, true, s.text.DeleteRead, func() { meta(c, s.text.DeleteReadHelp) }, func() {
			b := theme.Button{Style: theme.Danger, Small: true, Disabled: s.purging, Busy: s.purging, Text: s.text.DeleteReadButton,
				Label: s.text.DeleteReadTitle}.Draw(c)
			if b.Clicked() {
				s.countReadEntries()
			}
		})
	}}}
	if s.deps.ResetDevice != nil {
		rows = append(rows, cardRow{key: "reset-device", draw: func() {
			setting(c, icons.RotateCcw, true, s.text.ResetDevice, func() { meta(c, s.text.ResetDeviceHelp) }, func() {
				b := theme.Button{Style: theme.Danger, Small: true, Text: s.text.ResetDeviceButton, Label: s.text.ResetDeviceTitle}.Draw(c)
				if b.Clicked() {
					s.askReset = true
				}
			})
		}})
	}
	cardOf(c, rows)
}

// resetDialog asks before resetting this device. Escape cancels; nothing changes until Reset is chosen.
func (s *Screen) resetDialog(c *ui.Context) {
	if !s.askReset {
		return
	}
	if ui.AlertDialog(c, &s.askReset, s.text.ResetDeviceTitle, s.text.ResetDeviceBody, s.text.Cancel, s.text.ResetDeviceConfirm) == 1 {
		s.deps.ResetDevice()
	}
}

// countReadEntries reads how many entries would go, so that the confirmation can name the number.
func (s *Screen) countReadEntries() {
	if s.api == nil || s.purging {
		return
	}
	s.purging = true
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		count, err := api.ReadEntryCount(context.Background())
		return func() {
			if gen != s.gen {
				return
			}
			s.purging = false
			switch {
			case s.expired(err):
			case err != nil:
				s.toast = s.text.DeleteReadFailed
			case count == 0:
				s.toast = s.text.DeleteReadNone
			default:
				s.purgeCount, s.askPurge = count, true
			}
		}
	})
}

// deleteReadDialog asks before deleting. Escape cancels; nothing is deleted until Delete is chosen.
func (s *Screen) deleteReadDialog(c *ui.Context) {
	if !s.askPurge {
		return
	}
	body := locale.Count(s.text.DeleteReadBody, "n", int(s.purgeCount))
	if ui.AlertDialog(c, &s.askPurge, s.text.DeleteReadTitle, body, s.text.Cancel, s.text.DeleteReadConfirm) == 1 {
		s.deleteReadEntries()
	}
}

// deleteReadEntries has the server delete the read entries, then refreshes the figures and the timeline behind.
func (s *Screen) deleteReadEntries() {
	if s.api == nil || s.purging {
		return
	}
	s.purging = true
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		deleted, err := api.DeleteReadEntries(context.Background())
		return func() {
			if gen != s.gen {
				return
			}
			s.purging = false
			if s.expired(err) {
				return
			}
			if err != nil {
				s.toast = s.text.DeleteReadFailed
				return
			}
			s.actLoads++ // a read that began before the delete would bring the old figures back
			s.actBusy, s.actAt = false, time.Time{}
			s.toast = locale.Count(s.text.DeleteReadDone, "n", int(deleted))
			s.deps.Imported()
		}
	})
}

// preMigrateRow shows the copy of the database made before the last update with its size and the version it was made
// from, and the button that deletes it. There is no row without a copy.
func (s *Screen) preMigrateRow(c *ui.Context) {
	copyInfo := s.storage.PreMigrate
	if copyInfo == nil {
		return
	}
	value := s.bytes(copyInfo.Bytes)
	if copyInfo.Version > 0 {
		value = s.fill(s.fill(s.text.PreMigrateVersion, "{size}", value), "{version}", strconv.Itoa(copyInfo.Version))
	}
	setting(c, icons.RotateCcw, false, s.text.PreMigrate, func() { meta(c, value).FontFeatures("tnum") }, func() {
		drop := theme.Button{Style: theme.Danger, Small: true, Disabled: s.dropping, Text: s.text.DeletePreMigrate,
			Label: s.text.DeletePreMigrateTitle}.Draw(c)
		if drop.Clicked() {
			s.dropCopy = true
		}
	})
}

// dropCopyDialog asks to delete the copy. Escape cancels; nothing is deleted until Delete is chosen.
func (s *Screen) dropCopyDialog(c *ui.Context) {
	if !s.dropCopy {
		return
	}
	if ui.AlertDialog(c, &s.dropCopy, s.text.DeletePreMigrateTitle, s.text.DeletePreMigrateBody, s.text.Cancel,
		s.text.DeletePreMigrateConfirm) == 1 {
		s.deletePreMigrate()
	}
}

// deletePreMigrate has the server delete the copy; the row goes once it has.
func (s *Screen) deletePreMigrate() {
	if s.api == nil || s.dropping {
		return
	}
	s.dropping = true
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		err := api.DeletePreMigrate(context.Background())
		return func() {
			if gen != s.gen {
				return
			}
			s.dropping = false
			if s.expired(err) {
				return
			}
			if err != nil {
				s.toast = s.text.PreMigrateFailed
				return
			}
			s.storage.PreMigrate = nil
			s.actLoads++ // a read that began before the delete would bring the row back
			s.actBusy, s.actAt = false, s.deps.Now()
			s.toast = s.text.PreMigrateDeleted
		}
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
	sectionTitle(c, s.text.Diagnostics, gap(t))
	card(c, func() {
		setting(c, icons.FileText, false, s.text.DiagnosticsFile, func() { meta(c, s.text.DiagnosticsHelp) }, func() {
			label := s.text.Export
			if s.diagBusy {
				label = s.text.ExportingDiagnostics
			}
			export := theme.Button{Disabled: s.diagBusy, Icon: icons.Download, Text: label, Label: s.text.ExportDiagnostics,
				Busy: s.diagBusy}.Draw(c)
			if export.Clicked() {
				s.exportDiagnostics()
			}
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

// keyValue is a setting row that shows a value, in Text2 with tabular figures, where a control would be.
func (s *Screen) keyValue(c *ui.Context, icon *ui.SVG, label, value string) {
	t := c.Theme()
	setting(c, icon, false, label, nil, func() {
		ui.Text(c, value).SingleLine().TextColor(theme.Text2(t)).FontFeatures("tnum")
	})
}

func (s *Screen) fetchingNowCard(c *ui.Context) {
	t := c.Theme()
	sectionTitle(c, s.text.FetchingNow, gap(t))
	if len(s.activity.Fetching) == 0 {
		card(c, func() { meta(c, s.text.FetchingNone) })
		return
	}
	rows := make([]cardRow, len(s.activity.Fetching))
	for i, f := range s.activity.Fetching {
		rows[i] = cardRow{key: f.ID, draw: func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(theme.Px(t, tokens.Space3)).Children(func() {
				ui.Box(c).Size(theme.Px(t, tokens.IconM), theme.Px(t, tokens.IconM)).Center().Children(func() {
					spin := ui.Icon(c, icons.LoaderCircle).FontSize(theme.IconSmall(t)).TextColor(t.Accent)
					spin.Rotate(spin.Loop("spin", tokens.MotionSpin, ui.Linear) * 360)
				})
				ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(theme.Px(t, tokens.Space0_5)).Children(func() {
					ui.Text(c, sourceName(f.Title, f.RSSHubPath, f.URL)).SingleLine()
					line := s.fill(s.text.FetchingAttempt, "{n}", strconv.Itoa(f.Attempt))
					if f.Instance != "" {
						line += " · " + s.fill(s.text.FetchingVia, "{host}", f.Instance)
					}
					meta(c, line)
				})
				ui.Text(c, s.fill(s.text.FetchingElapsed, "{n}", strconv.FormatInt(f.ElapsedMs/1000, 10))).NoWrap().TextColor(theme.Text2(t))
			})
		}}
	}
	cardOf(c, rows)
}

func (s *Screen) recentCard(c *ui.Context) {
	t := c.Theme()
	sectionTitle(c, s.text.Recent, gap(t))
	if len(s.activity.Sources) == 0 {
		card(c, func() { meta(c, s.text.RecentNone) })
		return
	}
	now := s.deps.Now()
	plate(c).Children(func() {
		for i, src := range s.activity.Sources {
			ui.Column(c).Key(src.ID).FillWidth().Children(func() {
				if i > 0 {
					hairline(c)
				}
				s.sourceRow(c, src, now)
			})
		}
	})
	meta(c, locale.Count(s.text.RecentKeep, "n", s.activity.Keep)).
		Margin(theme.Px(t, tokens.Space2), theme.Px(t, tokens.Space1), 0, theme.Px(t, tokens.Space1))
}

// sourceRow is a subscription with its last fetch, which opens the list of its recent fetches. Only the open row draws
// them.
func (s *Screen) sourceRow(c *ui.Context, src client.ActivitySource, now time.Time) {
	t := c.Theme()
	open := s.actOpen == src.ID
	name := sourceName(src.Title, src.RSSHubPath, src.URL)
	ui.Column(c).Key(src.ID).FillWidth().Children(func() {
		row := ui.ButtonBase(c).FillWidth().MinHeight(theme.Px(t, tokens.Control+tokens.Space4)).AlignItems(ui.Center).
			Gap(theme.Px(t, tokens.Space3)).PaddingX(theme.Px(t, tokens.Space2)).Margin(0, -theme.Px(t, tokens.Space2)).
			Radius(theme.Px(t, tokens.RadiusControl)).Label(s.fill(s.text.ShowAttempts, "{name}", name)).Role(ui.RoleDisclosure).Expanded(open)
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
				ui.Text(c, name).SingleLine().MinWidth(0)
				ui.Text(c, summary).FontSize(theme.Px(t, tokens.TextMeta.Size.En)).TextColor(t.TextMuted).SingleLine().MinWidth(0)
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
			ui.Column(c).FillWidth().Padding(0, 0, theme.Px(t, tokens.Space2), theme.Px(t, tokens.IconS+tokens.Space3)).
				Children(func() {
					for i, a := range src.Attempts {
						s.attemptLine(c, i, a, now)
					}
				})
		}
	})
}

// The widths of an attempt's columns after its time: the HTTP status, how long it took and what it brought.
const (
	colStatus  = 40
	colTook    = 56
	colBrought = 84
)

// attemptLine is one fetch: how it went, when, the HTTP status, how long it took and what it brought; a failed one
// says why on a second line.
func (s *Screen) attemptLine(c *ui.Context, i int, a client.FetchAttempt, now time.Time) {
	t := c.Theme()
	ui.Column(c).Key(i).FillWidth().Children(func() {
		size := theme.Px(t, tokens.TextMeta.Size.En)
		ui.Row(c).FillWidth().MinHeight(theme.Px(t, tokens.Row)).AlignItems(ui.Center).Gap(theme.Px(t, tokens.Space3)).
			Children(func() {
				s.resultIcon(c, a.OK)
				ui.Text(c, s.ago(a.At, now)).FontSize(size).Grow(1).Shrink(1).MinWidth(0).SingleLine()
				ui.Text(c, s.status(a)).SingleLine().FontSize(size).TextColor(t.TextMuted).FontFeatures("tnum").
					Width(theme.Px(t, colStatus))
				ui.Text(c, s.duration(a.DurationMs)).SingleLine().FontSize(size).TextColor(t.TextMuted).FontFeatures("tnum").
					Width(theme.Px(t, colTook))
				ui.Text(c, s.outcome(a)).SingleLine().FontSize(size).FontFeatures("tnum").Width(theme.Px(t, colBrought))
			})
		if !a.OK && a.Error != "" {
			meta(c, a.Error).Margin(0, 0, theme.Px(t, tokens.Space1), theme.Px(t, tokens.IconS+tokens.Space3))
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
