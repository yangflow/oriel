package settings

import "testing"

// TestSelectTriggerHugsContent shows a popup-menu button in Settings is as wide as its label, gap and chevron, not a
// fixed 160: the run of the button's fill across the label's line stays well under 160 for a short label.
func TestSelectTriggerHugsContent(t *testing.T) {
	s, _, tt := openAppearance(t, "en")
	label, ok := tt.Find(s.text.SystemFont)
	if !ok {
		t.Fatalf("no %q on the page: %v", s.text.SystemFont, tt.Texts())
	}
	img := tt.Image()
	y := int(label.Y + label.H/2)
	// The row's fill is the card's; the button is the run of a different fill around the label.
	card := img.RGBAAt(int(label.X)-40, y)
	left, right := int(label.X), int(label.X+label.W)
	for left > 0 && img.RGBAAt(left-1, y) != card {
		left--
	}
	for right < img.Bounds().Dx()-1 && img.RGBAAt(right+1, y) != card {
		right++
	}
	if w := right - left; w < 40 || w >= 140 {
		t.Errorf("the select trigger is %d wide around %v; want it to hug the label (under 140)", w, label)
	}
}
