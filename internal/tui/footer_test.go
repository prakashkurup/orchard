package tui

import (
	"strings"
	"testing"
)

func TestPackHints(t *testing.T) {
	opts := []string{cmdHint("a", "alpha"), cmdHint("b", "beta"), cmdHint("c", "gamma")}
	tail := []string{cmdHint("?", "help")}

	// Too narrow for any opt: the tail must still be present.
	narrow := packHints(1, opts, tail)
	if !strings.Contains(narrow, "help") {
		t.Error("tail must always be present, even when nothing else fits")
	}
	if strings.Contains(narrow, "alpha") {
		t.Error("no opts should fit at width 1")
	}

	// Plenty of room: everything shows, in order, ending with the tail.
	wide := packHints(1000, opts, tail)
	for _, w := range []string{"alpha", "beta", "gamma", "help"} {
		if !strings.Contains(wide, w) {
			t.Errorf("expected %q at wide width", w)
		}
	}
	if strings.Index(wide, "gamma") > strings.Index(wide, "help") {
		t.Error("tail must come after the packed opts")
	}
}
