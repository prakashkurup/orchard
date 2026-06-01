package tui

import (
	"strings"
	"testing"
)

// A crafted working tree must not be able to inject terminal escapes through the
// diff viewer; tabs (indentation) must survive. Control bytes are built via
// rune() so the source file itself stays pure ASCII.
func TestSanitizeDiffLine(t *testing.T) {
	esc := string(rune(0x1b)) // ESC - starts CSI / OSC
	bel := string(rune(0x07)) // BEL - terminates OSC
	cr := string(rune(0x0d))  // carriage return
	tab := string(rune(0x09)) // tab - must be kept
	c1 := string(rune(0x9b))  // C1 CSI

	cases := []struct{ in, want string }{
		{"+ normal code", "+ normal code"},
		{"+" + tab + "indented", "+" + tab + "indented"},                   // tab preserved
		{"+ " + esc + "[31mred" + esc + "[0m", "+ [31mred[0m"},             // ESC stripped, text kept
		{"+ title" + esc + "]0;pwned" + bel + "end", "+ title]0;pwnedend"}, // OSC: ESC+BEL stripped, text kept
		{"- with" + cr + "carriage", "- withcarriage"},                     // CR stripped
		{"+ bell" + bel + "ok", "+ bellok"},                                // BEL stripped
		{"+ c1" + c1 + "ctrl", "+ c1ctrl"},                                 // C1 stripped
	}
	for _, c := range cases {
		got := sanitizeDiffLine(c.in)
		if got != c.want {
			t.Errorf("sanitizeDiffLine(%q) = %q, want %q", c.in, got, c.want)
		}
		if strings.ContainsRune(got, 0x1b) {
			t.Errorf("sanitizeDiffLine(%q) left an ESC byte: %q", c.in, got)
		}
	}
}
