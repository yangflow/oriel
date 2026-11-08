package fetch

import (
	"context"
	"slices"
	"strconv"
	"time"

	"github.com/yangflow/oriel/internal/store"
)

// The automatic fetch interval, docs/product/fetching.md "抓取".
const (
	// DefaultInterval is the interval of a new source, and of an automatic one with too little history.
	DefaultInterval = 15 * time.Minute
	// MaxAutoInterval is the longest automatic interval; the shortest is DefaultInterval.
	MaxAutoInterval = 24 * time.Hour
	// AutoSample is how many of the newest dated entries the interval is worked out from.
	AutoSample = 20
	// AutoMinEntries is the fewest dated entries an automatic interval is worked out from.
	AutoMinEntries = 5
)

// The global fetch defaults, docs/product/fetching.md "全局默认".
const (
	// IntervalSetting holds the default interval in seconds; 0 or nothing is automatic.
	IntervalSetting = "default_fetch_interval_sec"
	// FullTextSetting says whether new subscriptions fetch the full text; only "0" is off.
	FullTextSetting = "default_full_text"
	// MinFixedInterval and MaxFixedInterval bound an interval that is set by hand.
	MinFixedInterval = time.Minute
	MaxFixedInterval = 7 * 24 * time.Hour
)

// GlobalInterval is the default interval that automatic sources follow, 0 for automatic. A stored value that is not
// a valid interval counts as automatic.
func GlobalInterval(ctx context.Context, st *store.Store) (time.Duration, error) {
	raw, err := st.Setting(ctx, IntervalSetting)
	if err != nil {
		return 0, err
	}
	sec, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, nil
	}
	d := time.Duration(sec) * time.Second
	if d < MinFixedInterval || d > MaxFixedInterval {
		return 0, nil
	}
	return d, nil
}

// FullTextDefault is whether a new subscription fetches the full text.
func FullTextDefault(ctx context.Context, st *store.Store) (bool, error) {
	raw, err := st.Setting(ctx, FullTextSetting)
	if err != nil {
		return false, err
	}
	return raw != "0", nil
}

// AutoInterval is the median gap between the given publication times, kept between DefaultInterval and
// MaxAutoInterval. Only the newest AutoSample times count, and with fewer than AutoMinEntries it is DefaultInterval.
func AutoInterval(published []time.Time) time.Duration {
	times := slices.Clone(published)
	slices.SortFunc(times, func(a, b time.Time) int { return b.Compare(a) })
	times = times[:min(len(times), AutoSample)]
	if len(times) < AutoMinEntries {
		return DefaultInterval
	}
	gaps := make([]time.Duration, 0, len(times)-1)
	for i := 1; i < len(times); i++ {
		gaps = append(gaps, times[i-1].Sub(times[i]))
	}
	slices.Sort(gaps)
	median := gaps[len(gaps)/2]
	if len(gaps)%2 == 0 {
		median = (gaps[len(gaps)/2-1] + gaps[len(gaps)/2]) / 2
	}
	return min(max(median, DefaultInterval), MaxAutoInterval)
}

// Thresholds of SuggestKind.
const (
	suggestMinEntries = 3 // fewer entries say too little
	suggestImages     = 2 // pictures that make an entry picture-heavy
)

// SuggestKind is the kind the entries of a first fetch look like, or "" when they look like the current kind or like
// nothing in particular. A feed whose entries are mostly videos with a duration suggests "video"; one whose entries
// mostly carry several pictures suggests "image". "Mostly" is more than half, of at least three entries. It only
// suggests; nothing applies it.
func SuggestKind(entries []store.FeedEntry, current string) string {
	if len(entries) < suggestMinEntries {
		return ""
	}
	var videos, pictures int
	for _, e := range entries {
		if e.DurationSec > 0 {
			videos++
		}
		if len(e.Images) >= suggestImages {
			pictures++
		}
	}
	switch {
	case videos*2 > len(entries):
		if current != "video" {
			return "video"
		}
	case pictures*2 > len(entries):
		if current != "image" {
			return "image"
		}
	}
	return ""
}
