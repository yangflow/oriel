package settings

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/ui/icons"
	"github.com/yangflow/oriel/internal/ui/locale"
	"github.com/yangflow/oriel/internal/ui/theme"
)

// readerEvery is how often the dialog asks the server how the import goes while it reads or writes.
const readerEvery = 500 * time.Millisecond

// readerServices are the readers the dialog can import from, in the order of its choice.
var readerServices = []struct{ code, name string }{
	{client.ServiceMiniflux, "Miniflux"},
	{client.ServiceFreshRSS, "FreshRSS"},
}

// reader is the Import from Reader dialog: the form, then the progress of reading, the preview, the progress of
// writing and the result. The credential lives in cred only until Connect and is never kept after it.
type reader struct {
	open       bool
	focus      bool // the address field takes the focus
	service    int  // the index in readerServices
	base, user string
	cred       string
	starting   bool // the request to start is under way
	started    bool // the server has a job for this dialog
	confirming bool // the request to write is under way
	polling    bool
	failure    string // the code of why the import failed, "" for none
	status     client.ImportStatus
	at         time.Time // when the server was last asked
	run        int       // counts attempts, so the answer to one the user left is dropped
	told       bool      // Imported has been called
	scroll     ui.ScrollState
}

// The steps of the dialog.
const (
	stepForm = iota
	stepReading
	stepPreview
	stepWriting
	stepDone
	stepFailed
)

func (r *reader) step() int {
	switch {
	case r.failure != "":
		return stepFailed
	case r.starting || !r.started:
		if r.starting {
			return stepReading
		}
		return stepForm
	}
	switch r.status.Phase {
	case "ready":
		return stepPreview
	case "writing":
		return stepWriting
	case "done":
		return stepDone
	}
	return stepReading
}

// validAddress is whether raw is an http or https address with a host, as the server takes it.
func validAddress(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != ""
}

// ready is whether the form has what Connect needs.
func (r *reader) ready() bool {
	if strings.TrimSpace(r.cred) == "" || !validAddress(r.base) {
		return false
	}
	return readerServices[r.service].code != client.ServiceFreshRSS || strings.TrimSpace(r.user) != ""
}

func (s *Screen) openReader() {
	if s.api == nil {
		return
	}
	r := s.reader
	s.reader = reader{open: true, focus: true, service: r.service, base: r.base, user: r.user, run: r.run + 1}
}

// closeReader closes the dialog and has the server drop the import, or clear a finished one.
func (s *Screen) closeReader() {
	r := s.reader
	s.reader = reader{service: r.service, base: r.base, user: r.user, run: r.run + 1}
	if r.started {
		s.cancelRemote()
	}
}

// retryReader goes back to the form, with the address and user name as they were and no credential.
func (s *Screen) retryReader() {
	r := s.reader
	s.reader = reader{open: true, focus: true, service: r.service, base: r.base, user: r.user, run: r.run + 1}
	if r.started {
		s.cancelRemote()
	}
}

func (s *Screen) cancelRemote() {
	if s.api == nil {
		return
	}
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		_, err := api.CancelImport(context.Background())
		return func() {
			if gen == s.gen {
				s.expired(err)
			}
		}
	})
}

// connectReader sends the credential to the server once, to read the other reader into a preview.
func (s *Screen) connectReader() {
	r := &s.reader
	if s.api == nil || r.starting || !r.ready() {
		return
	}
	req := client.ImportRequest{Service: readerServices[r.service].code, BaseURL: strings.TrimSpace(r.base),
		Credential: strings.TrimSpace(r.cred)}
	if req.Service == client.ServiceFreshRSS {
		req.Username = strings.TrimSpace(r.user)
	}
	r.cred, r.starting, r.failure = "", true, ""
	r.run++
	gen, run, api := s.gen, r.run, s.api
	s.deps.Go(func() func() {
		status, err := api.StartImport(context.Background(), req)
		return func() { s.readerStarted(gen, run, status, err) }
	})
}

func (s *Screen) readerStarted(gen, run int, status client.ImportStatus, err error) {
	if gen != s.gen || s.expired(err) {
		return
	}
	r := &s.reader
	if run != r.run {
		if err == nil {
			s.cancelRemote() // the user left before the answer came
		}
		return
	}
	r.starting = false
	if err != nil {
		s.readerFailed(code(err))
		return
	}
	r.started, r.status, r.at = true, status, s.deps.Now()
}

func (s *Screen) readerFailed(reason string) {
	s.reader.failure = reason
	if s.reader.failure == "" {
		s.reader.failure = "other"
	}
	s.announce = s.readerError(s.reader.failure)
}

// readerError is the sentence for the code of why an import failed.
func (s *Screen) readerError(reason string) string {
	switch reason {
	case "unauthorized":
		return s.text.ReaderErrUnauthorized
	case "unreachable":
		return s.text.ReaderErrUnreachable
	case "rate_limited":
		return s.text.ReaderErrRate
	case "bad_response":
		return s.text.ReaderErrBad
	case "too_large":
		return s.text.ReaderErrLarge
	case "redirected":
		return s.text.ReaderErrRedirect
	case "invalid_base_url":
		return s.text.ReaderErrAddress
	case "import_busy":
		return s.text.ReaderErrBusy
	}
	return s.text.ReaderErrOther
}

// watchReader asks for the state of the import once its time has come, and wakes the screen for the next time. It
// runs only while the dialog waits on the server.
func (s *Screen) watchReader(c *ui.Context) {
	r := &s.reader
	if s.api == nil || !r.started || r.polling || r.confirming || r.failure != "" {
		return
	}
	if phase := r.status.Phase; phase != "fetching" && phase != "writing" {
		return
	}
	if wait := readerEvery - s.deps.Now().Sub(r.at); wait > 0 {
		c.After(wait)
		return
	}
	r.polling, r.at = true, s.deps.Now()
	gen, run, api := s.gen, r.run, s.api
	s.deps.Go(func() func() {
		status, err := api.ImportStatus(context.Background())
		return func() {
			if gen != s.gen || run != s.reader.run {
				return
			}
			s.reader.polling = false
			if s.expired(err) || err != nil {
				return // a failed poll is asked again
			}
			s.readerStatus(status)
		}
	})
}

// readerStatus takes the state the server reports.
func (s *Screen) readerStatus(status client.ImportStatus) {
	r := &s.reader
	r.status = status
	switch status.Phase {
	case "failed":
		s.readerFailed(status.Error)
	case "canceled", "idle":
		s.closeReader()
	case "done":
		if res := status.Result; res != nil {
			if !r.told && res.AddedSources+res.StarredAdded+res.ReadMarked > 0 {
				s.deps.Imported()
			}
			s.announce = s.sourcesResult(res)
		}
		r.told = true
	}
}

// confirmReader has the server write what the preview found.
func (s *Screen) confirmReader() {
	r := &s.reader
	if s.api == nil || r.confirming || r.status.Phase != "ready" {
		return
	}
	r.confirming = true
	gen, run, api := s.gen, r.run, s.api
	s.deps.Go(func() func() {
		status, err := api.ConfirmImport(context.Background())
		return func() {
			if gen != s.gen || s.expired(err) || run != s.reader.run {
				return
			}
			s.reader.confirming = false
			if err != nil {
				s.readerFailed(code(err))
				return
			}
			s.reader.at = s.deps.Now()
			s.readerStatus(status)
		}
	})
}

// readerDialog draws the dialog while it is open.
func (s *Screen) readerDialog(c *ui.Context) {
	r := &s.reader
	if !r.open {
		return
	}
	s.watchReader(c)
	t := c.Theme()
	_, height := c.Size()
	step := r.step()
	open := true
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(28, 24, 18, 0.32)).Padding(t.Space(4))
		panel.Column().Width(theme.Px(t, 440)).MaxHeight(height - t.Space(8)).Radius(16).Background(theme.RaisedFill(t)).
			Label(s.text.ReaderTitle)
		sheet(c, panel, 20, 50)
		line := func(shown bool) {
			color := t.Border
			if !shown {
				color = color.Alpha(0)
			}
			ui.Box(c).FillWidth().Height(1).Background(color)
		}
		s.readerHeader(c, &open)
		line(r.scroll.Y > 0)
		ui.Scroll(c).TrackScroll(&r.scroll).FillWidth().Shrink(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Gap(t.Space(3.5)).Padding(t.Space(3.5), t.Space(5), t.Space(1), t.Space(5)).
				Children(func() {
					switch step {
					case stepForm:
						s.readerForm(c)
					case stepReading, stepWriting:
						s.readerProgress(c)
					case stepPreview:
						s.readerPreview(c)
					case stepDone:
						s.readerResult(c)
					default:
						s.readerFailure(c)
					}
				})
		})
		line(r.scroll.Y < r.scroll.MaxY)
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Padding(t.Space(4), t.Space(5), t.Space(4.5),
			t.Space(5)).Children(func() {
			ui.Box(c).Grow(1)
			s.readerButtons(c, step)
		})
	})
	if !open {
		s.closeReader()
	}
}

func (s *Screen) readerHeader(c *ui.Context, open *bool) {
	t := c.Theme()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(3)).Padding(t.Space(4.5), t.Space(5), t.Space(1),
		t.Space(5)).Children(func() {
		ui.Text(c, s.text.ReaderTitle).FontSize(theme.Px(t, 15)).FontWeight(600).Grow(1).MinWidth(0).Role(ui.RoleHeading).Level(2)
		x := theme.Fade(ui.ButtonBase(c).Size(theme.Px(t, 28), theme.Px(t, 28)).Radius(8).AlignSelf(ui.Start).
			Label(s.text.ReaderClose).Tooltip(s.text.ReaderClose))
		color := t.TextMuted
		switch {
		case x.Pressed():
			x.Background(t.SurfacePressed)
			color = t.Text
		case x.Hovered():
			x.Background(t.SurfaceHover)
			color = t.Text
		}
		x.Children(func() { ui.Icon(c, icons.X).FontSize(theme.Px(t, 16)).TextColor(color) })
		if x.Clicked() {
			*open = false
		}
	})
}

func (s *Screen) readerButtons(c *ui.Context, step int) {
	r := &s.reader
	pick := func(style theme.ButtonStyle, disabled bool, text string) bool {
		b := theme.Button{Style: style, Disabled: disabled, Text: text}.Draw(c)
		return b.Clicked()
	}
	switch step {
	case stepForm:
		if pick(theme.Secondary, false, s.text.ReaderCancel) {
			s.closeReader()
		}
		if pick(theme.Primary, !r.ready(), s.text.ReaderConnect) {
			s.connectReader()
		}
	case stepReading, stepWriting:
		if pick(theme.Secondary, false, s.text.ReaderCancel) {
			s.closeReader()
		}
	case stepPreview:
		if pick(theme.Secondary, r.confirming, s.text.ReaderCancel) {
			s.closeReader()
		}
		if pick(theme.Primary, r.confirming, s.text.ReaderImport) {
			s.confirmReader()
		}
	case stepDone:
		if pick(theme.Primary, false, s.text.ReaderDone) {
			s.closeReader()
		}
	default:
		if pick(theme.Secondary, false, s.text.ReaderClose) {
			s.closeReader()
		}
		if pick(theme.Primary, false, s.text.ReaderTryAgain) {
			s.retryReader()
		}
	}
}

// readerForm is the service, its address and the credential.
func (s *Screen) readerForm(c *ui.Context) {
	t := c.Theme()
	r := &s.reader
	names := make([]string, len(readerServices))
	for i, svc := range readerServices {
		names[i] = svc.name
	}
	ui.Column(c).FillWidth().Gap(theme.Px(t, 6)).Children(func() {
		ui.Text(c, s.text.ReaderService).FontSize(theme.Px(t, 12)).FontWeight(500)
		ui.Row(c).Children(func() {
			if i, changed := s.segments(c, s.text.ReaderService, names, r.service); changed {
				r.service = i
				r.cred = ""
			}
		})
	})
	submit := func(in ui.Element) {
		if in.Submitted() {
			s.connectReader()
		}
	}
	trimmed := strings.TrimSpace(r.base)
	bad := trimmed != "" && !validAddress(trimmed) && (strings.Contains(trimmed, "://") || strings.Contains(trimmed, "."))
	ui.Column(c).FillWidth().Gap(theme.Px(t, 6)).Children(func() {
		ui.Text(c, s.text.ReaderAddress).FontSize(theme.Px(t, 12)).FontWeight(500)
		field(c, "reader-base", &r.base, 0, bad, func(in ui.Element) {
			in.Font("monospace").Label(s.text.ReaderAddress).Placeholder(s.text.ReaderAddressHint)
			if bad {
				in.Error(s.text.ReaderErrAddress)
			}
			if r.focus {
				in.Focus()
				r.focus = false
			}
			submit(in)
		})
		if bad {
			ui.Text(c, s.text.ReaderErrAddress).FontSize(theme.Px(t, 12)).TextColor(t.Danger).Wrap()
		}
	})
	freshrss := readerServices[r.service].code == client.ServiceFreshRSS
	if freshrss {
		ui.Column(c).FillWidth().Gap(theme.Px(t, 6)).Children(func() {
			ui.Text(c, s.text.ReaderUser).FontSize(theme.Px(t, 12)).FontWeight(500)
			field(c, "reader-user", &r.user, 0, false, func(in ui.Element) {
				in.Label(s.text.ReaderUser)
				submit(in)
			})
		})
	}
	credential := s.text.ReaderToken
	if freshrss {
		credential = s.text.ReaderPassword
	}
	ui.Column(c).FillWidth().Gap(theme.Px(t, 6)).Children(func() {
		ui.Text(c, credential).FontSize(theme.Px(t, 12)).FontWeight(500)
		field(c, "reader-credential", &r.cred, 0, false, func(in ui.Element) {
			in.Password().Label(credential).Description(s.text.ReaderMemory)
			submit(in)
		})
		ui.Text(c, s.text.ReaderMemory).FontSize(theme.Px(t, 12)).TextColor(t.TextMuted).Wrap()
	})
}

// readerProgress is the stage being read or written, how far it is and a bar.
func (s *Screen) readerProgress(c *ui.Context) {
	t := c.Theme()
	r := &s.reader
	p := r.status.Progress
	stage := s.text.ReaderStageSubscriptions
	switch p.Stage {
	case "starred":
		stage = s.text.ReaderStageStarred
	case "read":
		stage = s.text.ReaderStageRead
	case "writing":
		stage = s.text.ReaderStageWriting
	}
	if r.step() == stepWriting {
		stage = s.text.ReaderStageWriting
	}
	value, how := -1.0, ""
	switch {
	case p.Total > 0:
		value = min(float64(p.Done)/float64(p.Total), 1)
		how = s.fill(s.text.ReaderProgress, "{done}", strconv.Itoa(p.Done), "{total}", strconv.Itoa(p.Total))
	case p.Done > 0:
		how = s.fill(s.text.ReaderProgressOpen, "{done}", strconv.Itoa(p.Done))
	}
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Role(ui.RoleStatus).Children(func() {
		spin := ui.Icon(c, icons.LoaderCircle).FontSize(theme.Px(t, 14)).TextColor(t.TextMuted)
		spin.Rotate(spin.Loop("spin", 900*time.Millisecond, ui.Linear) * 360)
		ui.Text(c, stage).Shrink(1).Wrap()
	})
	ui.Progress(c, value).FillWidth().Label(stage)
	if how != "" {
		ui.Text(c, how).FontSize(theme.Px(t, 12)).TextColor(t.TextMuted)
	}
}

// count writes a number with a comma between its thousands.
func count(n int) string {
	text := strconv.Itoa(n)
	for i := len(text) - 3; i > 0; i -= 3 {
		text = text[:i] + "," + text[i:]
	}
	return text
}

// readerPreview is what was found, with nothing written yet.
func (s *Screen) readerPreview(c *ui.Context) {
	t := c.Theme()
	counts := s.reader.status.Counts
	ui.Text(c, s.fill(s.text.ReaderFound, "{service}", readerServices[s.reader.service].name)).Wrap()
	row := func(label string, n int, note string) {
		ui.Column(c).FillWidth().Role(ui.RoleListItem).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(3)).Children(func() {
				ui.Text(c, label).Grow(1).Shrink(1).MinWidth(0)
				ui.Text(c, count(n)).FontWeight(500).NoWrap()
			})
			if note != "" {
				ui.Text(c, note).FontSize(theme.Px(t, 12)).TextColor(t.TextMuted).Wrap()
			}
		})
	}
	ui.Column(c).FillWidth().Gap(theme.Px(t, 8)).Role(ui.RoleList).Children(func() {
		row(s.text.ReaderSources, counts.Sources, "")
		row(s.text.ReaderStarred, counts.Starred, "")
		row(s.text.ReaderRead, counts.Read, "")
		if counts.Folders > 0 {
			row(s.text.ReaderFolders, counts.Folders, s.text.ReaderFoldersNote)
		}
	})
}

// readerResult is what was added and what was left out.
func (s *Screen) readerResult(c *ui.Context) {
	t := c.Theme()
	res := s.reader.status.Result
	if res == nil {
		return
	}
	lines := []string{
		s.sourcesResult(res),
		locale.Count(locale.Count(s.text.ReaderResultStarred, "added", res.StarredAdded), "existing", res.StarredExisting),
		locale.Count(s.text.ReaderResultRead, "n", res.ReadMarked),
	}
	if res.UnmatchedRead > 0 {
		lines = append(lines, locale.Count(s.text.ReaderResultUnmatched, "n", res.UnmatchedRead))
	}
	if res.FoldersSkipped > 0 {
		lines = append(lines, locale.Count(s.text.ReaderResultFolders, "n", res.FoldersSkipped))
	}
	tone := done
	if res.SkippedTotal > 0 || res.UnmatchedRead > 0 || res.FoldersSkipped > 0 {
		tone = partly
	}
	s.noteBox(c, tone, lines)
	if len(res.Skipped) == 0 {
		return
	}
	ui.Column(c).FillWidth().Gap(theme.Px(t, 6)).Children(func() {
		ui.Text(c, s.text.ReaderSkipped).FontSize(theme.Px(t, 12)).FontWeight(500).Role(ui.RoleHeading).Level(3)
		ui.Column(c).FillWidth().Label(s.text.ReaderSkipped).Role(ui.RoleList).Children(func() {
			for i, item := range res.Skipped {
				s.skippedRow(c, i, item)
			}
		})
		if more := res.SkippedTotal - len(res.Skipped); more > 0 {
			ui.Text(c, s.fill(s.text.ReaderMore, "{n}", strconv.Itoa(more))).FontSize(theme.Px(t, 12)).TextColor(t.TextMuted)
		}
	})
}

func (s *Screen) skippedRow(c *ui.Context, i int, item client.ImportSkip) {
	t := c.Theme()
	why := map[string]string{
		"invalid_url": s.text.InvalidURL, "invalid_rsshub_path": s.text.InvalidRoute, "invalid_kind": s.text.InvalidKind,
		"invalid_source": s.text.InvalidSource, "unknown_source": s.text.ReaderUnknownSource,
		"invalid_link": s.text.ReaderInvalidLink,
	}[item.Error]
	if why == "" {
		why = item.Error
	}
	ui.Column(c).Key(i).FillWidth().Padding(theme.Px(t, 3), 0).Role(ui.RoleListItem).Children(func() {
		spans := []ui.Span{}
		if title := strings.TrimSpace(item.Title); title != "" {
			spans = append(spans, ui.Span{Text: title + " · "})
		}
		spans = append(spans, ui.Span{Text: item.Address, Font: "monospace", Size: theme.Px(t, 12)})
		ui.RichText(c, spans...).FillWidth().FontSize(theme.Px(t, 12)).SingleLine()
		ui.Text(c, why).FontSize(theme.Px(t, 12)).TextColor(t.TextMuted).Wrap()
	})
}

// readerFailure is why the import stopped.
func (s *Screen) readerFailure(c *ui.Context) {
	s.noteBox(c, refused, []string{s.readerError(s.reader.failure)})
}

// noteBox is a tinted box of lines with the icon of its tone, as the results under Import and Export are.
func (s *Screen) noteBox(c *ui.Context, tone int, lines []string) {
	t := c.Theme()
	fill, icon, iconColor, color := theme.Ink(t, 0.045), icons.Check, t.Success, t.Text
	switch tone {
	case partly:
		fill, icon, iconColor = theme.Soft(t, t.Warning), icons.TriangleAlert, t.Warning
	case refused:
		fill, icon, iconColor, color = theme.Soft(t, t.Danger), icons.CircleAlert, t.Danger, t.Danger
	}
	ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(t.Space(2)).Padding(theme.Px(t, 10), t.Space(3)).Radius(t.Radius).
		Background(fill).Role(ui.RoleStatus).Children(func() {
		ui.Icon(c, icon).FontSize(theme.Px(t, 14)).TextColor(iconColor).Margin(2, 0, 0, 0)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(theme.Px(t, 2)).Children(func() {
			for i, line := range lines {
				ui.Text(c, line).Key(i).FontSize(theme.Px(t, 12)).TextColor(color).Wrap()
			}
		})
	})
}

// sourcesResult says how many subscriptions of the reader service were added and how many were here already.
func (s *Screen) sourcesResult(res *client.ImportResult) string {
	return locale.Count(locale.Count(s.text.ReaderResultSources, "added", res.AddedSources), "existing", res.ExistingSources)
}
