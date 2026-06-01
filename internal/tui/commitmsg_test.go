package tui

import (
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
)

func TestCleanCommitMsg(t *testing.T) {
	cases := []struct{ in, want string }{
		{"feat: add thing", "feat: add thing"},
		{"  feat: add thing\n\n", "feat: add thing"},
		{"```\nfeat: add thing\n```", "feat: add thing"},
		{"```text\nfeat: add thing\n```", "feat: add thing"},
		{"feat: add thing\n\nLonger body line.", "feat: add thing\n\nLonger body line."},
		{"", ""},
	}
	for _, c := range cases {
		if got := cleanCommitMsg(c.in); got != c.want {
			t.Errorf("cleanCommitMsg(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestWrapText(t *testing.T) {
	// No line exceeds the width, and a blank line between subject and body survives.
	msg := "feat(checkout): apply promo codes to the order summary\n\n" +
		"Wire the promo field into buildCheckoutSummary and format the discounted total in SummaryCard so customers can see savings before paying."
	const width = 40
	lines := wrapText(msg, width)
	for i, ln := range lines {
		if w := runewidth.StringWidth(ln); w > width {
			t.Errorf("line %d width %d > %d: %q", i, w, width, ln)
		}
	}
	if !strings.Contains(strings.Join(lines, "\n"), "\n\n") {
		t.Errorf("expected a preserved blank line between subject and body, got:\n%s", strings.Join(lines, "\n"))
	}
	// A single word longer than width is kept whole on its own line (not dropped).
	long := wrapText("https://example.com/a/very/long/unbreakable/url/that/exceeds", 20)
	if len(long) != 1 || long[0] == "" {
		t.Errorf("long unbreakable word should be one non-empty line, got %q", long)
	}
}
