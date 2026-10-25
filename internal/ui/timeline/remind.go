package timeline

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/ui/locale"
)

// A reminder choice is a time worked out here in local time and sent as an absolute time (docs/product.md "稍后提醒我").
type remindChoice struct {
	name string
	at   time.Time
}

// remindChoices are the times a reminder can be set for, counted from now: in an hour, this evening (18:00, or the
// evening of tomorrow when it is past), tomorrow at 09:00 and next Monday at 09:00.
func (s *Screen) remindChoices(now time.Time) []remindChoice {
	at := func(days, hour int) time.Time {
		y, m, d := now.Date()
		return time.Date(y, m, d+days, hour, 0, 0, 0, now.Location())
	}
	evening := at(0, 18)
	if !evening.After(now) {
		evening = at(1, 18)
	}
	ahead := (int(time.Monday) - int(now.Weekday()) + 7) % 7
	if ahead == 0 {
		ahead = 7
	}
	return []remindChoice{
		{s.text.RemindHour, now.Add(time.Hour)},
		{s.text.RemindEvening, evening},
		{s.text.RemindTomorrow, at(1, 9)},
		{s.text.RemindNextWeek, at(ahead, 9)},
	}
}

// remindWhen is the day and time of a reminder, as shown on a row and announced.
func (s *Screen) remindWhen(ms int64) string {
	return locale.DateTime(s.lang, s.hours, s.deps.Now(), time.UnixMilli(ms))
}

// remind sets a reminder on the entry id: it counts as read and stays out of Unread until at, then comes back as
// unread at the top of the list.
func (s *Screen) remind(id int64, at time.Time) {
	ms, read := at.UnixMilli(), true
	if s.stay.id == id {
		s.stay.done = true
	}
	s.change(id, client.EntryPatch{IsRead: &read, RemindAt: &ms})
	s.announce = strings.ReplaceAll(s.text.RemindSet, "{time}", s.remindWhen(ms))
}

// unremind cancels the reminder of the entry id and leaves it read.
func (s *Screen) unremind(id int64) {
	none := int64(0)
	s.change(id, client.EntryPatch{RemindAt: &none})
	s.announce = s.text.RemindCancelled
}

// remindTarget is the entry the palette sets a reminder on: the one in the reader, else the one chosen in the list.
func (s *Screen) remindTarget() (client.Entry, bool) {
	i := s.index(s.reading)
	if s.reading == 0 {
		i = s.selected
	}
	if i < 0 || i >= len(s.entries) {
		return client.Entry{}, false
	}
	return s.entries[i], true
}

// remindMenu fills the submenu "Remind me later" of the entry e: the choices, and "Cancel reminder" while it has one.
func (s *Screen) remindMenu(m *ui.Menu, e client.Entry) {
	for _, choice := range s.remindChoices(s.deps.Now()) {
		if m.Item(choice.name).Chosen() {
			s.remind(e.ID, choice.at)
		}
	}
	if e.RemindAt > 0 {
		m.Separator()
		if m.Item(s.text.RemindCancel).Chosen() {
			s.unremind(e.ID)
		}
	}
}
