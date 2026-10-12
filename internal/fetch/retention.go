package fetch

import (
	"context"
	"log/slog"
	"strconv"
	"time"
)

const (
	// RetentionSetting holds how many days entries are kept; 0 or unset keeps them forever.
	RetentionSetting = "retention_days"
	maxRetentionDays = 3650
	pruneEvery       = time.Hour
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
	return time.Duration(min(days, maxRetentionDays)) * 24 * time.Hour
}

// prune deletes expired, unstarred entries at most once an hour.
func (p *Poller) prune(ctx context.Context) {
	now := p.now()
	if !p.lastPrune.IsZero() && now.Sub(p.lastPrune) < pruneEvery {
		return
	}
	keep := p.retention(ctx)
	if keep == 0 {
		return
	}
	deleted, err := p.store.PruneEntries(ctx, now.Add(-keep))
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
}
