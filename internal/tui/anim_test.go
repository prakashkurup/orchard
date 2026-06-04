package tui

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/prakashkurup/orchard/internal/repo"
)

func nonBlankGridLines(m model, w int) int {
	n := 0
	for _, ln := range strings.Split(m.renderGrid(w), "\n") {
		if strings.TrimSpace(ansiPattern.ReplaceAllString(ln, "")) != "" {
			n++
		}
	}
	return n
}

func TestRowRevealOnce(t *testing.T) {
	m := newModel("root", 4)
	m.width, m.height = 100, 30
	m.resize()
	for i := 0; i < 8; i++ {
		m.repos = append(m.repos, repo.Repo{Name: fmt.Sprintf("repo-%d", i), Path: fmt.Sprintf("/r/%d", i)})
	}
	m.syncRows()

	m.beginReveal()
	if !m.revealActive || !m.revealed {
		t.Fatal("beginReveal should start the cascade")
	}
	early := nonBlankGridLines(m, 100) // frame 0: nothing has cascaded in yet
	for i := 0; i < (m.revealLines+4)*revealPerRow && m.revealActive; i++ {
		m.stepAnims()
	}
	if m.revealActive {
		t.Fatal("reveal never completed")
	}
	full := nonBlankGridLines(m, 100)
	if early >= full {
		t.Fatalf("rows should cascade in (early=%d, full=%d)", early, full)
	}

	// It must NOT replay on a later refresh/filter/sort.
	m.beginReveal()
	if m.revealActive {
		t.Error("the cascade must only play once per process")
	}

	// With animation off, rows appear instantly (no cascade).
	t.Setenv("ORCHARD_NO_ANIM", "1")
	m2 := newModel("root", 4)
	m2.width, m2.height = 100, 30
	m2.resize()
	m2.repos = append(m2.repos, repo.Repo{Name: "x", Path: "/x"})
	m2.syncRows()
	m2.beginReveal()
	if m2.revealActive {
		t.Error("with animation off, rows should appear instantly")
	}
}

func TestSpring1dConverges(t *testing.T) {
	s := newSpring1d(8.0, 1.0)
	s.setNow(0)
	s.to(10)
	moving, steps := true, 0
	for moving && steps < 600 {
		moving = s.step()
		steps++
	}
	if moving {
		t.Fatal("spring never settled")
	}
	if math.Abs(s.pos-10) > 0.001 {
		t.Fatalf("spring settled at %.3f, want 10", s.pos)
	}
	// setNow is instantaneous, no motion
	s.setNow(3)
	if s.active || s.step() {
		t.Error("setNow should leave the spring at rest")
	}
}

func TestIntroPlaysThenStops(t *testing.T) {
	in := newIntro(100, 24)
	steps := 0
	for in.step() {
		steps++
		if steps > introFrames*2 {
			t.Fatal("intro never finished")
		}
	}
	if steps+1 < introFrames {
		t.Fatalf("intro ended early after %d frames, want ~%d", steps+1, introFrames)
	}
	// mid-play, the grove should be drawing trees (a trunk on screen)
	mid := newIntro(80, 20)
	for i := 0; i < introFrames/2; i++ {
		mid.step()
	}
	if !strings.Contains(ansiPattern.ReplaceAllString(mid.view(80, 20), ""), "┃") {
		t.Error("expected tree trunks mid-intro")
	}
}

func TestIntroViewWidthSafe(t *testing.T) {
	in := newIntro(120, 30)
	for f := 0; f < introFrames; f += 7 {
		for n := 0; n < 7; n++ {
			in.step()
		}
		for _, w := range []int{60, 120, 200} {
			for _, ln := range strings.Split(in.view(w, 20), "\n") {
				if got := lipgloss.Width(ln); got != w {
					t.Fatalf("frame %d width %d: a line is %d wide (banding/overflow)", in.frame, w, got)
				}
			}
		}
	}
}

func TestCountUpAndDisable(t *testing.T) {
	m := newModel("root", 4)
	m.repos = make([]repo.Repo, 7)

	m.beginCountUp() // animation on by default: starts from zero, climbs
	if m.shownRepoCount() != 0 {
		t.Fatalf("count-up should start at 0, got %d", m.shownRepoCount())
	}
	for i := 0; i < 600 && m.repoCount.active; i++ {
		m.stepAnims()
	}
	if m.shownRepoCount() != 7 {
		t.Fatalf("count-up should land on 7, got %d", m.shownRepoCount())
	}

	t.Setenv("ORCHARD_NO_ANIM", "1")
	m2 := newModel("root", 4)
	m2.repos = make([]repo.Repo, 5)
	m2.beginCountUp()
	if m2.repoCount.active || m2.shownRepoCount() != 5 {
		t.Fatalf("with animation off the count should be exact immediately, got %d active=%v", m2.shownRepoCount(), m2.repoCount.active)
	}
}

func TestPulseDecays(t *testing.T) {
	m := newModel("root", 4)
	if cmd := m.pulse("/x/acme"); cmd == nil {
		t.Fatal("pulse should arm the ticker")
	}
	if m.pulses["/x/acme"] != pulseFrames {
		t.Fatalf("pulse should start at %d frames, got %d", pulseFrames, m.pulses["/x/acme"])
	}
	for i := 0; i < pulseFrames+5 && len(m.pulses) > 0; i++ {
		m.stepAnims()
	}
	if len(m.pulses) != 0 {
		t.Fatalf("pulse should fully decay, %d left", len(m.pulses))
	}

	t.Setenv("ORCHARD_NO_ANIM", "1")
	m2 := newModel("root", 4)
	if cmd := m2.pulse("/x/acme"); cmd != nil || len(m2.pulses) != 0 {
		t.Error("with animation off, pulse should be a no-op")
	}
}
