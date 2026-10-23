package fetch

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/yangflow/oriel/internal/store"
)

const (
	// RetentionSetting holds how many days entries are kept; 0 or unset keeps them forever.
	RetentionSetting = "retention_days"
	// MaxRetentionDays caps the retention setting, and a source's own keep period, at about ten years.
	MaxRetentionDays = 3650
	pruneEvery       = time.Hour
	day              = 24 * time.Hour
)

// retention is how long entries are kept, zero for forever. Values that are not a day count mean forever.
func (p *Poller) retention(ctx context.Context) time.Duration {
	raw, err := p.store.Setting(ctx, RetentionSetting)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("Cannot read the retention setting", "error", err)
		}
		return 0
	}
	days, err := strconv.Atoi(raw)
	if err != nil || days <= 0 {
		return 0
	}
	return time.Duration(min(days, MaxRetentionDays)) * day
}

// keepFor is how long the entries of a source are kept, zero for forever. The source's own keep period wins over the
// global one: KeepDays 0 follows it, -1 keeps forever, a positive number of days is used as it is.
func keepFor(src store.FeedSource, global time.Duration) time.Duration {
	switch {
	case src.KeepDays > 0:
		return time.Duration(min(src.KeepDays, MaxRetentionDays)) * day
	case src.KeepDays < 0:
		return 0
	}
	return global
}

// prune deletes expired, unstarred entries at most once an hour, each by the keep period of its source.
func (p *Poller) prune(ctx context.Context) {
	now := p.now()
	if !p.lastPrune.IsZero() && now.Sub(p.lastPrune) < pruneEvery {
		return
	}
	deleted, err := p.store.PruneEntries(ctx, now, int(p.retention(ctx)/day))
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("Cannot prune entries", "error", err)
		}
		return
	}
	p.lastPrune = now
	if deleted > 0 {
		slog.Info("Pruned expired entries", "count", deleted)
	}
	p.maintain(ctx, deleted)
	purged, entries, err := p.store.PurgeDeletedSources(ctx, now)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("Cannot remove deleted subscriptions", "error", err)
		}
		return
	}
	if purged > 0 {
		slog.Info("Removed deleted subscriptions", "count", purged, "entries", entries)
	}
	p.maintain(ctx, entries)
}

// maintain gives the disk space of removed entries back (see store.Maintain). A prune that was cut off by ctx, which
// is a shutdown, never gets here, so it skips this.
func (p *Poller) maintain(ctx context.Context, removed int64) {
	if err := p.store.Maintain(ctx, removed); err != nil && ctx.Err() == nil && !errors.Is(err, store.ErrVacuumRunning) {
		slog.Error("Cannot shrink the database after removing entries", "error", err)
	}
}
