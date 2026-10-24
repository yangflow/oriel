package fetch

import (
	"slices"
	"sync"
	"time"
)

const (
	// InstanceHealthKeep is how many of the latest requests to one RSSHub instance its health is counted over.
	InstanceHealthKeep = 50
	// InstanceHealthDays is how old, at most, a request may be to still count.
	InstanceHealthDays = 7
)

const instanceHealthAge = InstanceHealthDays * 24 * time.Hour

// InstanceHealth is what the latest requests to one RSSHub instance say about it. SuccessRate, MedianMs and
// CoolingUntil are nil when there is nothing to say: no request, no answered request, not cooling down.
type InstanceHealth struct {
	URL          string
	Requests     int
	SuccessRate  *float64 // answered / requests, 0 to 1
	MedianMs     *int64   // of the answered requests only
	CoolingUntil *time.Time
}

// healthSample is one request sent to an instance.
type healthSample struct {
	at       time.Time // when it was sent
	took     time.Duration
	answered bool
}

// instanceStats keeps the latest requests per RSSHub instance, in memory only. The zero value is ready to use.
type instanceStats struct {
	mu      sync.Mutex
	samples map[string][]healthSample // oldest first
}

// record adds a request to an instance, keeping only the latest InstanceHealthKeep.
func (s *instanceStats) record(base string, sample healthSample) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.samples == nil {
		s.samples = map[string][]healthSample{}
	}
	list := append(s.samples[base], sample)
	if len(list) > InstanceHealthKeep {
		list = slices.Delete(list, 0, len(list)-InstanceHealthKeep)
	}
	s.samples[base] = list
}

// read returns the health of each of instances in order, as of now, and forgets the instances not in the list.
func (s *instanceStats) read(instances []string, now time.Time) []InstanceHealth {
	s.mu.Lock()
	defer s.mu.Unlock()
	for base := range s.samples {
		if !slices.Contains(instances, base) {
			delete(s.samples, base)
		}
	}
	out := make([]InstanceHealth, 0, len(instances))
	for _, base := range instances {
		h := InstanceHealth{URL: base}
		var took []time.Duration
		for _, sample := range s.samples[base] {
			if now.Sub(sample.at) > instanceHealthAge {
				continue
			}
			h.Requests++
			if sample.answered {
				took = append(took, sample.took)
			}
		}
		if h.Requests > 0 {
			rate := float64(len(took)) / float64(h.Requests)
			h.SuccessRate = &rate
		}
		if len(took) > 0 {
			slices.Sort(took)
			mid := len(took) / 2
			median := took[mid]
			if len(took)%2 == 0 {
				median = (took[mid-1] + took[mid]) / 2
			}
			ms := (median + time.Millisecond/2).Milliseconds()
			h.MedianMs = &ms
		}
		out = append(out, h)
	}
	return out
}

// InstanceHealth returns the health of each of the RSSHub instances, in the order given, with the end of the
// cool-down of those that are cooling down.
func (p *Poller) InstanceHealth(instances []string) []InstanceHealth {
	now := p.now()
	out := p.stats.read(instances, now)
	for i := range out {
		if until, _, cooling := p.rsshub.cooling(out[i].URL, now); cooling {
			out[i].CoolingUntil = &until
		}
	}
	return out
}
