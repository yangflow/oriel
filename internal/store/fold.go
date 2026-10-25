package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"time"

	"modernc.org/sqlite"
)

// MaxDailyCap is the largest daily cap a subscription may have.
const MaxDailyCap = 1000

func init() {
	sqlite.MustRegisterDeterministicScalarFunction("local_day", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		ms, _ := args[0].(int64)
		return localDay(time.UnixMilli(ms)), nil
	})
}

// localDay names the calendar day of t in the server's time zone, as year, month and day in one number.
func localDay(t time.Time) int64 {
	year, month, day := t.In(time.Local).Date()
	return int64(year)*10000 + int64(month)*100 + int64(day)
}

// execer is what both the database and a transaction offer.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// refold folds the unread, unstarred entries of a source that are over its daily cap, from the entries created at since
// (Unix milliseconds) on; a source without a cap folds nothing. The entries of one local day are ranked by arrival, and
// the ones the mute keywords hide do not take a place. It only ever folds: unfolding happens when the entry is read
// or starred (a trigger) or when the cap changes (see UpdateSource). See docs/product.md "每日上限".
func refold(ctx context.Context, db execer, sourceID, since int64) error {
	_, err := db.ExecContext(ctx, `
		UPDATE entries SET folded = 1 WHERE folded = 0 AND is_read = 0 AND starred = 0 AND id IN (
			SELECT id FROM (
				SELECT e.id, s.daily_cap AS cap, row_number() OVER (PARTITION BY local_day(e.created_at) ORDER BY e.created_at, e.id) AS n
				FROM entries e JOIN sources s ON s.id = e.source_id
				WHERE e.source_id = ?1 AND s.daily_cap > 0 AND e.created_at >= ?2 AND `+shown+`
			) WHERE n > cap
		)`, sourceID, since)
	return err
}

// dayBefore is the start of the local day before the one now falls in, in Unix milliseconds: the earliest entries a
// fetch can still change the ranking of.
func dayBefore(now time.Time) int64 {
	now = now.In(time.Local)
	return time.Date(now.Year(), now.Month(), now.Day()-1, 0, 0, 0, 0, time.Local).UnixMilli()
}
