package settings

import (
	"context"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
)

func (f *fake) RSSHubHealth(context.Context) (client.RSSHubHealth, error) {
	f.healthReads++
	return f.health, f.healthErr
}

func ptr[T any](v T) *T { return &v }

// openHealth shows the Fetching group with three saved instances and the health the fake answers.
func openHealth(t *testing.T, language string, health client.RSSHubHealth) (*Screen, *fake, *ui.Tester) {
	t.Helper()
	f := &fake{
		settings: client.Settings{RSSHubBaseURLs: []string{"https://rsshub.example.net", "https://rsshub.backup.example", "http://localhost:1200"},
			RetentionDays: 30},
		health: health,
	}
	s := New(language, f.deps())
	s.Use(f, "reader.example.com", true)
	s.Open()
	tt := newTester(func(c *ui.Context) { s.View(c) }, 1000, 1200)
	tt.Frame()
	f.settle(tt)
	return s, f, tt
}

func healthData() client.RSSHubHealth {
	cooling := now.Add(3*time.Minute + 20*time.Second).UnixMilli()
	return client.RSSHubHealth{Keep: 50, Days: 7, Instances: []client.InstanceHealth{
		{URL: "https://rsshub.example.net", Requests: 50, SuccessRate: ptr(0.98), MedianMs: ptr(int64(420))},
		{URL: "https://rsshub.backup.example", Requests: 14, SuccessRate: ptr(0.66), MedianMs: ptr(int64(1800))},
		{URL: "http://localhost:1200", Requests: 3, SuccessRate: ptr(0.0), CoolingUntil: &cooling},
	}}
}

func TestHealthShows(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		s, f, tt := openHealth(t, language, healthData())
		if f.healthReads != 1 {
			t.Fatalf("%s: %d reads", language, f.healthReads)
		}
		for _, text := range []string{
			s.text.Health, s.fill(s.text.HealthHelp, "{keep}", "50", "{days}", "7"),
			"rsshub.example.net", "rsshub.backup.example", "localhost:1200", s.text.MainBadge,
			"98% · 420 ms · " + s.fill(s.text.LastN, "{n}", "50"),
			"66% · 1.8 s · " + s.fill(s.text.LastN, "{n}", "14"),
			"0% · " + s.text.NoAnswer + " · " + s.fill(s.text.LastN, "{n}", "3"),
			s.cooling(now.Add(3*time.Minute+20*time.Second).UnixMilli(), now),
		} {
			if !tt.HasText(text) {
				t.Errorf("%s: no %q in %v", language, text, tt.Texts())
			}
		}
		if tt.HasText(s.text.HealthEmpty) || tt.HasText(s.text.NoData) {
			t.Errorf("%s: shows the empty state: %v", language, tt.Texts())
		}
	}
	s, _, _ := openHealth(t, "en", healthData())
	if got := s.cooling(now.Add(3*time.Minute+20*time.Second).UnixMilli(), now); got != "Cooling down, about 4 minutes to retry" {
		t.Errorf("cooling = %q", got)
	}
	if got := s.cooling(now.Add(time.Minute+time.Second).UnixMilli(), now); got != "Cooling down, about 2 minutes to retry" {
		t.Errorf("cooling just over a minute = %q", got)
	}
	if got := s.cooling(now.Add(30*time.Second).UnixMilli(), now); got != s.text.CoolingSoon {
		t.Errorf("cooling under a minute = %q", got)
	}
	if got := s.cooling(now.Add(-time.Second).UnixMilli(), now); got != s.text.CoolingSoon {
		t.Errorf("cooling that ended = %q", got)
	}
}

func TestHealthOneRowWithoutData(t *testing.T) {
	health := healthData()
	health.Instances[1] = client.InstanceHealth{URL: "https://rsshub.backup.example"}
	s, _, tt := openHealth(t, "en", health)
	if !tt.HasText(s.text.NoData) || !tt.HasText("98% · 420 ms · "+s.fill(s.text.LastN, "{n}", "50")) {
		t.Errorf("texts %v", tt.Texts())
	}
	if tt.HasText(s.text.HealthEmpty) {
		t.Errorf("shows the empty line although an instance has data")
	}
}

func TestHealthEmpty(t *testing.T) {
	empty := client.RSSHubHealth{Keep: 50, Days: 7}
	for _, url := range []string{"https://rsshub.example.net", "https://rsshub.backup.example", "http://localhost:1200"} {
		empty.Instances = append(empty.Instances, client.InstanceHealth{URL: url})
	}
	for _, language := range []string{"en", "zh-Hans"} {
		s, _, tt := openHealth(t, language, empty)
		if !tt.HasText(s.text.Health) || !tt.HasText(s.text.HealthEmpty) {
			t.Errorf("%s: texts %v", language, tt.Texts())
		}
		for _, text := range []string{"rsshub.example.net", s.text.MainBadge} {
			if tt.HasText(text) {
				t.Errorf("%s: shows %q with no data: %v", language, text, tt.Texts())
			}
		}
	}
}

func TestHealthHiddenWithoutInstancesOrOnError(t *testing.T) {
	s, f, tt := openHealth(t, "en", client.RSSHubHealth{Keep: 50, Days: 7, Instances: []client.InstanceHealth{}})
	if tt.HasText(s.text.Health) || f.healthReads != 1 {
		t.Errorf("no instances: %d reads, %v", f.healthReads, tt.Texts())
	}

	f = &fake{settings: client.Settings{RSSHubBaseURLs: []string{"https://rsshub.example.net"}, RetentionDays: 30},
		health: healthData(), healthErr: context.DeadlineExceeded}
	s = New("en", f.deps())
	s.Use(f, "reader.example.com", true)
	s.Open()
	tt = newTester(func(c *ui.Context) { s.View(c) }, 1000, 1200)
	tt.Frame()
	f.settle(tt)
	tt.Frame()
	f.settle(tt)
	if tt.HasText(s.text.Health) || tt.HasText(s.text.Error) || f.healthReads != 1 {
		t.Errorf("failed read: %d reads, %v", f.healthReads, tt.Texts())
	}
}

func TestHealthReadWhenOpenedAndAfterSave(t *testing.T) {
	s, f, tt := openHealth(t, "en", healthData())
	for range 3 {
		tt.Frame()
		f.settle(tt)
	}
	if f.healthReads != 1 {
		t.Fatalf("read again without reason: %d", f.healthReads)
	}

	// Saving the instances reads the health of the new list; until it arrives the old rows are not shown.
	edit(tt, func() { s.base = "https://rsshub.example.net\nhttps://rsshub.third.example" })
	f.health = client.RSSHubHealth{Keep: 50, Days: 7, Instances: []client.InstanceHealth{
		{URL: "https://rsshub.example.net", Requests: 5, SuccessRate: ptr(1.0), MedianMs: ptr(int64(90))},
		{URL: "https://rsshub.third.example"},
	}}
	press(t, tt, s.text.Save)
	work := f.queue[0]
	f.queue = f.queue[1:]
	work()()
	tt.Frame()
	if tt.HasText("localhost:1200") || tt.HasText("98% · 420 ms · "+s.fill(s.text.LastN, "{n}", "50")) {
		t.Errorf("shows stale rows: %v", tt.Texts())
	}
	f.settle(tt)
	if f.healthReads != 2 || !tt.HasText("100% · 90 ms · "+s.fill(s.text.LastN, "{n}", "5")) || !tt.HasText("rsshub.third.example") {
		t.Errorf("after save: %d reads, %v", f.healthReads, tt.Texts())
	}

	// Leaving the group and coming back reads it again.
	s.OpenPage("appearance")
	tt.Frame()
	f.settle(tt)
	s.OpenPage("fetching")
	tt.Frame()
	f.settle(tt)
	if f.healthReads != 3 {
		t.Errorf("reads after coming back: %d", f.healthReads)
	}
}

func TestHealthStaleResultDropped(t *testing.T) {
	s, f, tt := openHealth(t, "en", healthData())
	f.health = client.RSSHubHealth{Keep: 50, Days: 7, Instances: []client.InstanceHealth{{URL: "https://rsshub.example.net"}}}
	s.fetchHealth()
	older := f.queue[0]
	f.health = healthData()
	s.fetchHealth()
	f.queue = f.queue[1:]
	newer := f.queue[0]
	f.queue = nil
	newerResult := newer()
	older()() // the older answer lands after the newer one was asked for
	newerResult()
	tt.Frame()
	if len(s.health.Instances) != 3 {
		t.Errorf("a result passed by a newer request was applied: %+v", s.health)
	}
}

func TestHealthFormats(t *testing.T) {
	for rate, want := range map[float64]string{0: "0%", 0.004: "<1%", 0.01: "1%", 0.29: "29%", 0.6666: "66%", 0.995: "99%", 1: "100%"} {
		if got := percent(rate); got != want {
			t.Errorf("percent(%v) = %q, want %q", rate, got, want)
		}
	}
	for ms, want := range map[int64]string{0: "0 ms", 420: "420 ms", 999: "999 ms", 1000: "1.0 s", 1800: "1.8 s", 25000: "25.0 s"} {
		if got := latency(ms); got != want {
			t.Errorf("latency(%d) = %q, want %q", ms, got, want)
		}
	}
	if got := hostOf("https://rsshub.example.net"); got != "rsshub.example.net" {
		t.Errorf("hostOf = %q", got)
	}
}
