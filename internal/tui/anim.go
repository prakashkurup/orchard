package tui

import (
	"math"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/harmonica"
)

// One unified 60fps ticker drives every spring animation (launch intro, the
// REPOS count-up, "just tended" row pulses). It is armed only while something is
// moving and stops itself once everything settles, so an idle dashboard does no
// work. ORCHARD_NO_ANIM=1 turns all of it off. (Pager scrolling is deliberately
// instant, not eased: a spring settle adds lag to line-by-line navigation.)

type animTickMsg struct{}

const animFPS = 60

func animTick() tea.Cmd {
	return tea.Tick(time.Second/animFPS, func(time.Time) tea.Msg { return animTickMsg{} })
}

// animEnabled reports whether motion is on. Off via ORCHARD_NO_ANIM, matching
// the opt-out style of the update check and idle screensaver.
func animEnabled() bool {
	switch strings.ToLower(os.Getenv("ORCHARD_NO_ANIM")) {
	case "1", "true", "yes", "on":
		return false
	}
	return true
}

// spring1d is a single value driven toward a target by harmonica spring physics.
// active stays true until the value reaches its target with near-zero velocity.
type spring1d struct {
	s        harmonica.Spring
	pos, vel float64
	target   float64
	active   bool
}

func newSpring1d(angularFreq, damping float64) spring1d {
	return spring1d{s: harmonica.NewSpring(harmonica.FPS(animFPS), angularFreq, damping)}
}

// to aims the spring at a new target and marks it moving.
func (p *spring1d) to(target float64) {
	p.target = target
	p.active = true
}

// setNow jumps to a value instantly with no motion (used when animation is off).
func (p *spring1d) setNow(v float64) {
	p.pos, p.vel, p.target, p.active = v, 0, v, false
}

// step advances one frame and reports whether the spring is still moving.
func (p *spring1d) step() bool {
	if !p.active {
		return false
	}
	p.pos, p.vel = p.s.Update(p.pos, p.vel, p.target)
	if math.Abs(p.target-p.pos) < 0.4 && math.Abs(p.vel) < 0.4 {
		p.pos, p.vel, p.active = p.target, 0, false
	}
	return p.active
}

// startAnim arms the ticker if it is not already running, returning the tick cmd
// (or nil if already ticking, so we never stack two tickers).
func (m *model) startAnim() tea.Cmd {
	if m.animOn {
		return nil
	}
	m.animOn = true
	return animTick()
}

// stepAnims advances every active animation one frame and reports whether any is
// still in motion. It also applies side effects (pulse and count-up re-renders)
// and hands off from the intro to the count-up on finish.
func (m *model) stepAnims() bool {
	moving := false

	if m.intro != nil {
		if m.intro.step() {
			moving = true
		} else {
			// the orchard finishes growing: reveal the dashboard with the count-up
			// and a top-to-bottom cascade of the repo rows.
			m.intro = nil
			m.beginCountUp()
			m.beginReveal()
		}
	}

	if m.repoCount.step() {
		moving = true
	}

	if m.revealActive {
		m.revealFrame++
		m.syncRows() // re-render the grid so more rows appear
		if m.revealFrame >= (m.revealLines+2)*revealPerRow {
			m.revealActive = false
			m.syncRows()
		}
		moving = true
	}

	if len(m.pulses) > 0 {
		for path, n := range m.pulses {
			if n <= 1 {
				delete(m.pulses, path)
			} else {
				m.pulses[path] = n - 1
			}
		}
		if m.mode == modeList {
			m.syncRows() // re-render the grid so the pulse animates
		}
		if len(m.pulses) > 0 {
			moving = true
		}
	}

	return moving
}

// revealPerRow is how many frames each grid row waits before the next appears.
const revealPerRow = 2

// beginReveal staggers the repo rows in top-to-bottom, once per process. It is a
// no-op on refresh/filter/sort (guarded by m.revealed) so the list only ever
// cascades in on the first dashboard appearance.
func (m *model) beginReveal() {
	if !animEnabled() || m.revealed || len(m.view) == 0 {
		return
	}
	m.revealed = true
	m.revealActive = true
	m.revealFrame = 0
	m.revealLines = min(len(m.view), max(1, m.viewport.Height)) // only stagger what is visible
}

// beginCountUp animates the REPOS metric from zero up to the current repo count.
func (m *model) beginCountUp() {
	if !animEnabled() || len(m.repos) == 0 {
		m.repoCount.setNow(float64(len(m.repos)))
		return
	}
	m.repoCount.pos, m.repoCount.vel = 0, 0
	m.repoCount.to(float64(len(m.repos)))
}

// shownRepoCount is the REPOS metric value, animated while the count-up runs.
func (m model) shownRepoCount() int {
	if m.repoCount.active {
		return int(math.Round(m.repoCount.pos))
	}
	return len(m.repos)
}

// pulse marks a repo as freshly tended, kicking off a short blossom on its row.
func (m *model) pulse(path string) tea.Cmd {
	if !animEnabled() || path == "" {
		return nil
	}
	m.pulses[path] = pulseFrames
	return m.startAnim()
}

// usesDetailVP reports whether the mode scrolls the shared detail viewport, so
// line and page scrolling can be handled from one place.
func usesDetailVP(mode uiMode) bool {
	switch mode {
	case modeDetail, modeDiff, modeStats, modeHelp, modeWorklog, modePreview, modeCodeburn:
		return true
	}
	return false
}
