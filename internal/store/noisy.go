package store

import (
	"context"
	"time"
)

// DismissNoisy remembers the entries a day the source is at now, so the suggestion to quiet it stays away until the
// volume doubles. It returns ErrNotFound for an unknown source and ErrNotNoisy when the source is not named as noisy
// now; nothing is changed then. See docs/product.md "订阅健康".
func (s *Store) DismissNoisy(ctx context.Context, id int64, now time.Time) error {
	if _, err := s.Source(ctx, id); err != nil {
		return err
	}
	flagged, err := s.SourcesHealth(ctx, now)
	if err != nil {
		return err
	}
	for _, h := range flagged {
		if h.ID == id && h.State == HealthNoisy {
			_, err := s.db.ExecContext(ctx, "UPDATE sources SET noisy_dismissed = ? WHERE id = ?", h.PerDay, id)
			return err
		}
	}
	return ErrNotNoisy
}
