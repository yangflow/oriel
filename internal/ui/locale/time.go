package locale

import (
	_ "embed"
	"encoding/json"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

//go:embed copy.json
var catalog []byte

// words are the shared time texts of one language: the relative times and the layouts of dates and times of day.
type words struct {
	Now, Minutes, Hours, Yesterday, Days string
	DateLayout, YearLayout, DateTime     string
	Clock24, Clock12, AM, PM             string
}

var texts = func() map[string]words {
	var values map[string]words
	if err := json.Unmarshal(catalog, &values); err != nil {
		panic(err)
	}
	return values
}()

func wordsOf(lang string) words {
	if w, ok := texts[lang]; ok {
		return w
	}
	return texts["en"]
}

// Ago says how long before now a time was, as every screen writes it: "just now", "5 min ago", "3 h ago",
// "yesterday", "4 days ago", and from a week on the date (see Date). It is written for the middle of a line; Capital
// makes it fit the start of one. A time ahead of now reads as "just now".
func Ago(lang string, now, at time.Time) string {
	w := wordsOf(lang)
	at = at.In(now.Location())
	d := now.Sub(at)
	days := calendarDays(now, at)
	switch {
	case d < time.Minute:
		return w.Now
	case d < time.Hour:
		return Count(w.Minutes, "n", int(d/time.Minute))
	case days == 0:
		return Count(w.Hours, "n", int(d/time.Hour))
	case days == 1:
		return w.Yesterday
	case days < 7:
		return Count(w.Days, "n", days)
	}
	return Date(lang, now, at)
}

// calendarDays counts the midnights between at and now, in the zone of now.
func calendarDays(now, at time.Time) int {
	day := func(t time.Time) time.Time { y, m, d := t.Date(); return time.Date(y, m, d, 12, 0, 0, 0, time.UTC) }
	return int(day(now).Sub(day(at.In(now.Location()))).Hours() / 24)
}

// Date is the day of a time: month and day, with the year when it is not the year of now.
func Date(lang string, now, at time.Time) string {
	w := wordsOf(lang)
	at = at.In(now.Location())
	if at.Year() == now.Year() {
		return at.Format(w.DateLayout)
	}
	return at.Format(w.YearLayout)
}

// Clock is the time of day. hourCycle is the system's preference, 12 or 24; any other value (the system says
// nothing) takes the habit of the language: 12 hours in English, 24 in Chinese.
func Clock(lang string, hourCycle int, at time.Time) string {
	w := wordsOf(lang)
	if hourCycle != 12 && hourCycle != 24 {
		hourCycle = 12
		if lang == "zh-Hans" {
			hourCycle = 24
		}
	}
	if hourCycle == 24 {
		return at.Format(w.Clock24)
	}
	period := w.AM
	if at.Hour() >= 12 {
		period = w.PM
	}
	return strings.NewReplacer("{time}", at.Format("3:04"), "{period}", period).Replace(w.Clock12)
}

// DateTime is the day and the time of day of a time, as in "Jan 2, 3:04 PM".
func DateTime(lang string, hourCycle int, now, at time.Time) string {
	return strings.NewReplacer("{date}", Date(lang, now, at), "{time}", Clock(lang, hourCycle, at.In(now.Location()))).
		Replace(wordsOf(lang).DateTime)
}

// Capital turns the first letter to upper case, for a relative time that stands alone ("Yesterday").
func Capital(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}
