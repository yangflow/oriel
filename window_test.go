package main

import "testing"

// The main window remembers where the reader left it, and never opens below its minimum size.
func TestMainWindowOptions(t *testing.T) {
	o := mainWindowOptions(nil)
	if o.StateKey == "" {
		t.Fatal("the main window has no StateKey, so its size and position are forgotten")
	}
	if o.Width != 960 || o.Height != 640 {
		t.Errorf("default size = %dx%d, want 960x640", o.Width, o.Height)
	}
	if o.MinWidth != 640 || o.MinHeight != 480 {
		t.Errorf("minimum size = %dx%d, want 640x480", o.MinWidth, o.MinHeight)
	}
}
