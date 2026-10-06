package main

import "testing"

func TestWrap(t *testing.T) {
	got := wrap("calibre still has a USB Kindle connected. Eject it in calibre (or restart calibre) and tap again", 44, 3)
	if len(got) != 3 || got[0] != "calibre still has a USB Kindle connected." || got[2] != "" && len(got[2]) > 44 {
		t.Fatalf("%q", got)
	}
	if got := wrap("short", 44, 3); got[0] != "short" || got[1] != "" || got[2] != "" {
		t.Fatalf("%q", got)
	}
}
