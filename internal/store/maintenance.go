package store

import (
	"context"
	"log/slog"
	"strconv"
	"time"
)

const (
	// MaintainAfter is how many pruned entries make a prune worth a round of maintenance.
	MaintainAfter = 200
	// incrementalPages is how many free pages one incremental vacuum step gives back. Each step is its own short
	// write transaction, so a writer waits for one step at most.
	incrementalPages = 500
	// closeMaintenance bounds the work Close does before it closes the database.
	closeMaintenance = 10 * time.Second

	autoVacuumIncremental = 2 // the value of PRAGMA auto_vacuum for INCREMENTAL
)

// optimize lets SQLite refresh the statistics its query plans rely on; it is cheap and does nothing when they are fine.
func (s *Store) optimize(ctx context.Context) {
	if _, err := s.db.ExecContext(ctx, "PRAGMA optimize"); err != nil && ctx.Err() == nil {
		slog.Warn("Cannot optimize the database", "error", err)
	}
}

// Maintain gives the disk space of a large prune back and refreshes the query statistics. A prune that removed fewer
// than MaintainAfter entries leaves things as they are. A database made before incremental vacuum was used gets one
// full Vacuum, which converts it; after that the free pages go back in small steps that do not hold the write lock for
// long, then the write-ahead log is emptied. It returns ErrVacuumRunning when a vacuum is already running.
func (s *Store) Maintain(ctx context.Context, pruned int64) error {
	if pruned < MaintainAfter {
		return nil
	}
	var mode int
	if err := s.db.QueryRowContext(ctx, "PRAGMA auto_vacuum").Scan(&mode); err != nil {
		return err
	}
	if mode != autoVacuumIncremental {
		return s.Vacuum(ctx)
	}
	if !s.vacuuming.CompareAndSwap(false, true) {
		return ErrVacuumRunning
	}
	defer s.vacuuming.Store(false)
	for {
		var free int64
		if err := s.db.QueryRowContext(ctx, "PRAGMA freelist_count").Scan(&free); err != nil {
			return err
		}
		if free == 0 {
			break
		}
		if _, err := s.db.ExecContext(ctx, "PRAGMA incremental_vacuum("+strconv.Itoa(incrementalPages)+")"); err != nil {
			return err
		}
	}
	if _, err := s.db.ExecContext(ctx, checkpoint); err != nil {
		return err
	}
	s.optimize(ctx)
	return nil
}
