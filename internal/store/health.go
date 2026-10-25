package store

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"time"
)

// The thresholds of subscription health; docs/product.md ("订阅健康") gives the reasons.
const (
	healthSilentDays     = 90 // no new entry for this long makes a source dead
	healthFailingDays    = 30 // failing for this long makes a source dead
	healthMinFailures    = 3  // consecutive failures before a source counts as failing
	HealthWindowDays     = 30 // the window of the reading numbers, and the minimum age for "rarely read"
	healthMinRecent      = 10 // entries that arrived in the window before reading is judged
	healthReadPercentMax = 10 // a read share below this many percent is rarely read

	healthNoisyMinDays   = 7  // a subscription this old (whole days) can be noisy
	healthNoisyPerDay    = 20 // entries a day, on average, that make a source noisy
	healthNoisyReadMax   = 5  // a read share of at most this many percent, with that volume, is noisy
	healthNoisyRearmFold = 2  // a dismissed source is named again when its entries a day reach this many times the dismissed number
	// NoisyPaceSec is the pace the "slow down" action sets: Breaking, see docs/product.md "节奏".
	NoisyPaceSec = 3 * 60 * 60
)

// Health states and causes.
const (
	HealthDead       = "dead"
	HealthFailing    = "failing"
	HealthMoved      = "moved"
	HealthNoisy      = "noisy"
	HealthRarelyRead = "rarelyRead"

	HealthCauseFailing = "failing"
	HealthCauseSilent  = "silent"
)

// SourceHealth is a source that needs a decision.
type SourceHealth struct {
	ID                  int64
	URL                 string
	RSSHubPath          string
	Title               string
	IconURL             string
	Kind                string
	State               string // HealthDead, HealthFailing, HealthMoved, HealthNoisy or HealthRarelyRead
	Cause               string // for HealthDead, HealthCauseFailing or HealthCauseSilent; empty otherwise
	Days                int    // how many whole days the reason has held
	ConsecutiveFailures int
	LastError           string
	MovedTo             string // for HealthMoved, the address the site moved to
	Recent              int    // entries that arrived in the last HealthWindowDays, muted ones left out
	RecentRead          int
	RecentStarred       int
	PerDay              int // for HealthNoisy, entries a day over the days the source has existed (at most HealthWindowDays), rounded
	PaceSec             int // the source's pace, so a client knows whether "slow down" would change anything
	DailyCap            int
}

// ErrNotNoisy means the source is not named as noisy now.
var ErrNotNoisy = errors.New("store: source is not noisy")

const day = 24 * time.Hour

// SourcesHealth returns the sources that are dead, failing, moved or rarely read at now, worst first and by title within a
// state. Paused sources are left out. Three queries (the sources, the newest entry of each, the last window's counts
// of each) are merged here, so there is no subquery per source.
func (s *Store) SourcesHealth(ctx context.Context, now time.Time) ([]SourceHealth, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, url, rsshub_path, title, icon_url, kind, created_at, last_success_at, consecutive_failures, last_error,
			CASE WHEN moved_seen >= ?1 THEN moved_url ELSE '' END, daily_cap, pace_sec, noisy_dismissed
		FROM sources WHERE paused = 0 AND deleted_at IS NULL`, MovedSeenNeeded)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type candidate struct {
		SourceHealth
		created, success time.Time
		dismissed        int
	}
	var sources []candidate
	for rows.Next() {
		var c candidate
		var created int64
		var success sql.NullInt64
		if err := rows.Scan(&c.ID, &c.URL, &c.RSSHubPath, &c.Title, &c.IconURL, &c.Kind, &created, &success,
			&c.ConsecutiveFailures, &c.LastError, &c.MovedTo, &c.DailyCap, &c.PaceSec, &c.dismissed); err != nil {
			return nil, err
		}
		c.created = time.UnixMilli(created)
		if success.Valid {
			c.success = time.UnixMilli(success.Int64)
		}
		sources = append(sources, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if len(sources) == 0 {
		return []SourceHealth{}, nil
	}

	newest := map[int64]time.Time{}
	rows, err = s.db.QueryContext(ctx, "SELECT source_id, max(created_at) FROM entries GROUP BY source_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, at int64
		if err := rows.Scan(&id, &at); err != nil {
			return nil, err
		}
		newest[id] = time.UnixMilli(at)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	type counts struct{ recent, read, starred int }
	window := map[int64]counts{}
	rows, err = s.db.QueryContext(ctx, `
		SELECT e.source_id, count(*), sum(e.is_read), sum(e.starred)
		FROM entries e JOIN sources s ON s.id = e.source_id
		WHERE s.paused = 0 AND e.created_at >= ? AND `+shown+`
		GROUP BY e.source_id`, now.Add(-HealthWindowDays*day).UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var c counts
		if err := rows.Scan(&id, &c.recent, &c.read, &c.starred); err != nil {
			return nil, err
		}
		window[id] = c
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	wholeDays := func(since time.Time) int { return int(now.Sub(since) / day) }
	flagged := []SourceHealth{}
	for _, c := range sources {
		h := c.SourceHealth
		counted := window[h.ID]
		h.Recent, h.RecentRead, h.RecentStarred = counted.recent, counted.read, counted.starred

		failingSince := c.success
		if failingSince.IsZero() {
			failingSince = c.created
		}
		lastEntry, hasEntries := newest[h.ID]
		if !hasEntries {
			lastEntry = c.created
		}
		failing := h.ConsecutiveFailures >= healthMinFailures
		// A noisy source is judged over the days it has existed, at most the window; a dismissal holds until the volume
		// doubles.
		noisyDays := min(HealthWindowDays, wholeDays(c.created))
		var noisy, held bool
		if noisyDays >= healthNoisyMinDays {
			h.PerDay = (h.Recent + noisyDays/2) / noisyDays
			held = c.dismissed > 0 && h.PerDay < healthNoisyRearmFold*c.dismissed
			noisy = !held && h.DailyCap == 0 && h.Recent >= healthNoisyPerDay*noisyDays && h.RecentRead*100 <= h.Recent*healthNoisyReadMax
		}
		switch {
		case failing && wholeDays(failingSince) >= healthFailingDays:
			h.State, h.Cause, h.Days = HealthDead, HealthCauseFailing, wholeDays(failingSince)
		case wholeDays(lastEntry) >= healthSilentDays:
			h.State, h.Cause, h.Days = HealthDead, HealthCauseSilent, wholeDays(lastEntry)
		case failing:
			h.State = HealthFailing
		case h.MovedTo != "":
			h.State = HealthMoved
		case noisy:
			h.State, h.Days = HealthNoisy, noisyDays
		case !held && wholeDays(c.created) >= HealthWindowDays && h.Recent >= healthMinRecent && h.RecentRead*100 < h.Recent*healthReadPercentMax:
			h.State, h.Days = HealthRarelyRead, HealthWindowDays
		default:
			continue
		}
		if h.State != HealthMoved {
			h.MovedTo = ""
		}
		if h.State != HealthNoisy {
			h.PerDay = 0
		}
		flagged = append(flagged, h)
	}

	rank := map[string]int{HealthDead: 0, HealthFailing: 1, HealthMoved: 2, HealthNoisy: 3, HealthRarelyRead: 4}
	name := func(h SourceHealth) string {
		switch {
		case h.Title != "":
			return strings.ToLower(h.Title)
		case h.URL != "":
			return strings.ToLower(h.URL)
		}
		return strings.ToLower(h.RSSHubPath)
	}
	slices.SortFunc(flagged, func(a, b SourceHealth) int {
		if c := rank[a.State] - rank[b.State]; c != 0 {
			return c
		}
		if c := strings.Compare(name(a), name(b)); c != 0 {
			return c
		}
		return int(a.ID - b.ID)
	})
	return flagged, nil
}
