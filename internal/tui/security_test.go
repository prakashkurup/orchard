package tui

import "testing"

func TestIsWebURL(t *testing.T) {
	for _, u := range []string{"https://github.com/o/r", "http://x.test/a"} {
		if !isWebURL(u) {
			t.Errorf("isWebURL(%q) = false, want true", u)
		}
	}
	for _, u := range []string{
		"", "-W", "--help", "file:///etc/passwd", "ftp://x/y",
		"ssh://git@h/r", "git@github.com:o/r", "javascript:alert(1)",
	} {
		if isWebURL(u) {
			t.Errorf("isWebURL(%q) = true, want false (injection/scheme guard)", u)
		}
	}
}
