package timeline

import (
	"image"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/yangflow/oriel/internal/client"
)

func closeTo(a, b float32) bool { return math.Abs(float64(a-b)) < 0.01 }

func TestFadeRampsOverTheLastQuarter(t *testing.T) {
	e := client.Entry{FadesAt: now.Add(-time.Hour).UnixMilli(), ExpiresAt: now.Add(time.Hour).UnixMilli()}
	if got := fade(e, now.Add(-2*time.Hour)); got != 1 {
		t.Errorf("before it fades = %v", got)
	}
	if got := fade(e, now); !closeTo(got, 0.775) {
		t.Errorf("halfway = %v, want 0.775", got)
	}
	if got := fade(e, now.Add(time.Hour)); !closeTo(got, 0.55) {
		t.Errorf("at the end = %v, want 0.55", got)
	}
	if got := fade(e, now.Add(5*time.Hour)); !closeTo(got, 0.55) {
		t.Errorf("past the end = %v, want 0.55", got)
	}
	read := e
	read.IsRead = true
	if got := fade(read, now); got != 1 {
		t.Errorf("a read entry = %v, want 1", got)
	}
	if got := fade(client.Entry{}, now); got != 1 {
		t.Errorf("an entry with no pace = %v, want 1", got)
	}
}

// ink is how far the pixels of a region are from the colour of its corner: the more it is, the clearer the text.
func ink(img *image.RGBA, r image.Rectangle) float64 {
	r = r.Intersect(img.Bounds())
	bg := img.RGBAAt(r.Min.X, r.Min.Y)
	var sum float64
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			p := img.RGBAAt(x, y)
			sum += math.Abs(float64(p.R)-float64(bg.R)) + math.Abs(float64(p.G)-float64(bg.G)) + math.Abs(float64(p.B)-float64(bg.B))
		}
	}
	return sum
}

func TestRowAboutToLeaveIsFadedAndSaysSo(t *testing.T) {
	s, f, tt := setup(t, "en")
	tt.SetSize(1100, 900)
	tt.Frame()
	row := s.rects[0]
	region := image.Rect(int(row.X)+30, int(row.Y)+10, int(row.X+row.W)-80, int(row.Y+row.H)-10)
	full := ink(tt.Image(), region)
	if full == 0 || strings.Contains(s.label(s.entries[0], now), s.text.Leaving) {
		t.Fatalf("ink %v, label %q", full, s.label(s.entries[0], now))
	}

	f.entries[0].FadesAt = now.Add(-time.Hour).UnixMilli()
	f.entries[0].ExpiresAt = now.UnixMilli() + 1
	s.Refresh()
	f.settle(t, tt)
	tt.Frame()
	if !strings.HasSuffix(s.label(s.entries[0], now), s.text.Separator+s.text.Leaving) {
		t.Fatalf("label %q", s.label(s.entries[0], now))
	}
	faded := ink(tt.Image(), region)
	if faded >= full*0.8 || faded <= 0 {
		t.Errorf("ink %v of a faded row against %v of a full one", faded, full)
	}
	if strings.Contains(s.label(s.entries[1], now), s.text.Leaving) {
		t.Errorf("an entry with no pace says it leaves: %q", s.label(s.entries[1], now))
	}
}
