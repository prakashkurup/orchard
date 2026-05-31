package tui

import (
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
	"github.com/prakashkurup/orchard/internal/repo"
	"strings"
	"time"
	"unicode"
)

// relTime formats how long ago t was ("now", "5m", "3h", "2d", "4w", "6mo").
func relTime(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return relTimeSince(time.Since(t))
}

// relTimeSince is the pure duration→label core of relTime, taking the elapsed
// duration directly so it can be tested deterministically.
func relTimeSince(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dw", int(d.Hours()/(24*7)))
	default:
		return fmt.Sprintf("%dmo", int(d.Hours()/(24*30)))
	}
}

// freshnessColor maps a timestamp to a recency colour, so the SYNCED column
// reads as a heat-map (fresh = cool green/teal, stale = warm) instead of grey.
func freshnessColor(t time.Time) string {
	if t.IsZero() {
		return muted
	}
	return freshnessSince(time.Since(t))
}

// freshnessSince is the pure duration→colour core of freshnessColor.
func freshnessSince(d time.Duration) string {
	switch {
	case d < time.Hour:
		return green
	case d < 24*time.Hour:
		return teal
	case d < 7*24*time.Hour:
		return blue
	case d < 30*24*time.Hour:
		return yellow
	default:
		return orange
	}
}

// commitAgeColor maps a git "%cr" relative date ("3 days ago") to a recency
// colour for the LAST COMMIT prefix.
func commitAgeColor(rel string) string {
	switch {
	case rel == "now", strings.Contains(rel, "second"), strings.Contains(rel, "minute"):
		return green
	case strings.Contains(rel, "hour"):
		return teal
	case strings.Contains(rel, "day"):
		return blue
	case strings.Contains(rel, "week"):
		return yellow
	case strings.Contains(rel, "month"), strings.Contains(rel, "year"):
		return orange
	default:
		return muted
	}
}

// pluralSuffix returns "" for n==1 and "s" otherwise.
func pluralSuffix(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

type stateCounts struct {
	clean  int
	dirty  int
	behind int
	other  int
}

func countStates(repos []repo.Repo) stateCounts {
	var stats stateCounts
	for _, r := range repos {
		switch r.Display {
		case repo.DisplayClean:
			stats.clean++
		case repo.DisplayDirty:
			stats.dirty++
		case repo.DisplayBehind, repo.DisplayDiverged:
			stats.behind++
		default:
			stats.other++
		}
	}
	return stats
}

func cleanText(s string) string {
	var b strings.Builder
	lastSpace := false
	for _, r := range s {
		if r == '\t' || r == '\n' || r == '\r' || unicode.IsControl(r) {
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
			}
			continue
		}
		b.WriteRune(r)
		lastSpace = false
	}
	return strings.TrimSpace(b.String())
}

func padRight(s string, width int) string {
	s = fit(s, width)
	return s + strings.Repeat(" ", max(0, width-runewidth.StringWidth(s)))
}

func padLeft(s string, width int) string {
	s = fit(s, width)
	return strings.Repeat(" ", max(0, width-runewidth.StringWidth(s))) + s
}

func fit(s string, width int) string {
	s = cleanText(s)
	if width <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= width {
		return s
	}
	if width <= 3 {
		return runewidth.Truncate(s, width, "")
	}
	return runewidth.Truncate(s, width, "…")
}

// fitLeft truncates from the left with a leading ellipsis, so the meaningful
// tail (e.g. a file's basename) stays visible when a path is too long. Width is
// measured with ansi.StringWidth throughout to match ansi.TruncateLeft's metric,
// and the result is clamped so a wide rune straddling the cut cannot overflow.
func fitLeft(s string, width int) string {
	s = cleanText(s)
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= width {
		return s
	}
	ell := "…"
	if width <= 3 {
		ell = ""
	}
	target := width - ansi.StringWidth(ell)
	out := ansi.TruncateLeft(s, ansi.StringWidth(s)-target, "")
	for ansi.StringWidth(out) > target && len(out) > 0 {
		out = ansi.TruncateLeft(out, 1, "")
	}
	return ell + out
}

func clamp(n, lo, hi int) int {
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}

func sign(n int) int {
	switch {
	case n > 0:
		return 1
	case n < 0:
		return -1
	default:
		return 0
	}
}
