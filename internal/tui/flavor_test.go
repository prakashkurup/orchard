package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/prakashkurup/orchard/internal/repo"
)

func TestPushKonami(t *testing.T) {
	m := newModel("root", 8)
	seq := []string{"up", "up", "down", "down", "left", "right", "left", "right"}
	for i, k := range seq {
		got := m.pushKonami(k)
		if i < len(seq)-1 && got {
			t.Fatalf("konami triggered early at step %d", i)
		}
		if i == len(seq)-1 && !got {
			t.Fatal("konami did not trigger on the full sequence")
		}
	}
	m.pushKonami("up")
	m.pushKonami("x") // a non-arrow key resets the buffer
	if len(m.konami) != 0 {
		t.Fatal("a non-arrow key should reset the konami buffer")
	}
}

func TestBloomBand(t *testing.T) {
	for _, w := range []int{40, 120} {
		out := bloom(w, 3)
		if got := lipgloss.Width(out); got != w {
			t.Errorf("bloom(%d) width = %d, want %d", w, got, w)
		}
		if !strings.ContainsAny(ansiPattern.ReplaceAllString(out, ""), "✿❀❁✽❃") {
			t.Errorf("bloom(%d) should contain blossom glyphs", w)
		}
	}
}

func TestIsArborDay(t *testing.T) {
	// National Arbor Day 2026 is Friday, April 24 (last Friday of April).
	if !isArborDay(time.Date(2026, time.April, 24, 12, 0, 0, 0, time.UTC)) {
		t.Error("April 24, 2026 should be Arbor Day")
	}
	for _, d := range []time.Time{
		time.Date(2026, time.April, 23, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.April, 25, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC),
	} {
		if isArborDay(d) {
			t.Errorf("%s should not be Arbor Day", d.Format("2006-01-02"))
		}
	}
}

func TestGroveLabel(t *testing.T) {
	cases := map[int]string{0: "", 24: "", 25: "a grove", 99: "a grove", 100: "an orchard", 250: "an orchard"}
	for n, want := range cases {
		if got := groveLabel(n); got != want {
			t.Errorf("groveLabel(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestIsThriving(t *testing.T) {
	m := newModel("root", 8)
	if m.isThriving() {
		t.Error("an empty orchard is not thriving")
	}
	m.repos = []repo.Repo{{Display: repo.DisplayClean}, {Display: repo.DisplayAhead}, {Display: repo.DisplayFeature}}
	if !m.isThriving() {
		t.Error("clean / ahead / feature repos should be thriving")
	}
	m.repos = append(m.repos, repo.Repo{Display: repo.DisplayDirty})
	if m.isThriving() {
		t.Error("a dirty repo should break thriving")
	}
}

func TestTendingLineNonEmpty(t *testing.T) {
	if tendingLine() == "" {
		t.Error("tendingLine should never be empty")
	}
}

func TestCurrentSeason(t *testing.T) {
	d := func(mo time.Month) time.Time { return time.Date(2026, mo, 15, 12, 0, 0, 0, time.UTC) }
	north := map[time.Month]season{time.April: spring, time.July: summer, time.October: autumn, time.January: winter}
	for mo, want := range north {
		if got := currentSeason(d(mo), false); got != want {
			t.Errorf("north %s: got %d, want %d", mo, got, want)
		}
	}
	// southern hemisphere is offset by two seasons
	if got := currentSeason(d(time.July), true); got != winter {
		t.Errorf("south July: got %d, want winter", got)
	}
	if got := currentSeason(d(time.January), true); got != summer {
		t.Errorf("south January: got %d, want summer", got)
	}
}

func TestSeasonColorDistinct(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range []season{spring, summer, autumn, winter} {
		c := seasonColor(s)
		if c == "" {
			t.Errorf("seasonColor(%d) is empty", s)
		}
		seen[c] = true
	}
	if len(seen) != 4 {
		t.Errorf("expected 4 distinct season colors, got %d", len(seen))
	}
}

func TestScreensaverViewWidthAndBanding(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	for _, frame := range []int{0, 3, 9} {
		for _, ln := range strings.Split(screensaverView(64, 10, frame), "\n") {
			if w := ansi.StringWidth(ln); w != 64 {
				t.Errorf("frame %d: row width %d, want 64", frame, w)
			}
			if strings.Contains(ln, "\x1b[0m ") {
				t.Errorf("frame %d: unstyled gap (banding)", frame)
			}
		}
	}
}
