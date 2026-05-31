package tui

import (
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/prakashkurup/orchard/internal/repo"
)

const (
	accent    = "#C77DFF" // electric purple - selection marker, accents, modal borders
	brand     = "#FF4D6D" // candy red - ORCHARD wordmark
	blue      = "#82AAFF" // blue - branches, section headers, keys, info
	cyan      = "#7CE0FF" // cyan - sparing accents (stash, graph diagonals)
	teal      = "#5EE6D0" // teal - feature branches, fresh (hours)
	green     = "#B6F36A" // lime green - clean / ahead / success / fresh (minutes)
	yellow    = "#FFD56B" // yellow - dirty / warnings / aging (weeks)
	orange    = "#FF9E64" // orange - diverged / detached / Claude / stale (months)
	red       = "#FF5C7A" // coral red - behind / errors
	muted     = "#767DA8" // comment gray - dim text, separators (brighter, less washed)
	ice       = "#D5DAF2" // foreground (bright for contrast)
	bg        = "#15161F" // app background (deep slate)
	panel     = "#20243A" // elevated chips / modals
	panelDark = "#0F1016" // grid header / darkest
	rowAlt    = "#1B1E2C" // zebra row
	rowHot    = "#3E54AE" // selected row (vivid indigo - ties blue + purple)
	selFg     = "#F5F7FF" // selected-row text (bright, crisp)
	claudeC   = "#FF9E64" // orange - Claude accent
)

// Nerd Font glyphs (FontAwesome/Octicon range, present in any Nerd Font v3).
const (
	iconLogo    = "" // git square
	iconFolder  = "" // folder
	iconBranch  = "" // git branch
	iconClock   = "" // clock
	iconSync    = "" // refresh / sync
	iconCheck   = "" // check
	iconWarn    = "" // triangle warning
	iconArrowDn = "" // arrow down
	iconBolt    = "" // bolt
	iconRemote  = "" // cloud / remote
	iconCommit  = "" // git commit
)

var (
	appStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(ice)).
			Background(lipgloss.Color(bg)).
			Padding(1, 2)

	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(accent)).
			Background(lipgloss.Color(bg)).
			Bold(true)

	subtleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(muted)).
			Background(lipgloss.Color(bg))

	headerRuleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(muted)).
			Background(lipgloss.Color(bg))

	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(blue)).
			Background(lipgloss.Color(bg))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(red)).
			Background(lipgloss.Color(bg)).
			Bold(true)
)

func fillLine(s string, width int, bgc string) string {
	return lipgloss.NewStyle().
		Background(lipgloss.Color(bgc)).
		Width(width).
		MaxWidth(width).
		Render(s)
}

// hrule renders a full-width horizontal separator in the header-rule style.
func hrule(width int) string {
	return headerRuleStyle.Render(strings.Repeat("─", max(0, width)))
}

// modalBox builds a rounded, accent-bordered modal panel.
func modalBox(inner int, build func(add func(string))) string {
	var lines []string
	add := func(s string) { lines = append(lines, modalLine(s, inner)) }
	build(add)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(accent)).
		BorderBackground(lipgloss.Color(panel)).
		Background(lipgloss.Color(panel)).
		Padding(1, 3).
		Render(strings.Join(lines, "\n"))
}

// seg renders a fragment in fg on the app background.
func seg(fg, s string) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(fg)).Background(lipgloss.Color(bg)).Render(s)
}

// segB is the bold variant of seg.
func segB(fg, s string) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(fg)).Background(lipgloss.Color(bg)).Bold(true).Render(s)
}

// panelFG returns a style with foreground c on the panel (modal) background.
func panelFG(c string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Background(lipgloss.Color(panel))
}

// bgFG returns a style with foreground c on the app background.
func bgFG(c string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Background(lipgloss.Color(bg))
}

// modalLine forces a modal content line to exactly `inner` cells on the panel.
func modalLine(s string, inner int) string {
	return lipgloss.NewStyle().
		Background(lipgloss.Color(panel)).
		Width(inner).
		MaxWidth(inner).
		Render(s)
}

// hexRGB parses "#rrggbb" into its components.
func hexRGB(hex string) (int, int, int) {
	h := strings.TrimPrefix(hex, "#")
	if len(h) != 6 {
		return 0, 0, 0
	}
	v := func(s string) int { n, _ := strconv.ParseInt(s, 16, 0); return int(n) }
	return v(h[0:2]), v(h[2:4]), v(h[4:6])
}

// wordmark renders the ORCHARD wordmark. It shimmers during the bloom easter
// egg, and on Arbor Day it sprouts a seedling.
func (m model) wordmark() string {
	fg := seasonColor(currentSeason(time.Now(), southernHemisphere()))
	if m.bloomFrames > 0 {
		shimmer := []string{brand, accent, blue, teal, green, yellow, orange}
		fg = shimmer[(bloomFrames-m.bloomFrames)%len(shimmer)]
	}
	mark := bgFG(fg).Bold(true).Render("ORCHARD")
	if isArborDay(time.Now()) {
		mark += bgFG(green).Render(" 🌱")
	}
	return mark
}

func cellStyle(fg, bgc string, bold bool) lipgloss.Style {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(fg)).
		Background(lipgloss.Color(bgc))
	if bold {
		style = style.Bold(true)
	}
	return style
}

func infoColor(r repo.Repo, pulling bool) string {
	if pulling {
		return accent
	}
	if r.SkipReason != "" || r.Err != "" {
		return yellow
	}
	return muted
}

func selectionText(selected bool) string {
	if selected {
		return "[x]"
	}
	return "[ ]"
}

func selectionColor(selected bool) string {
	if selected {
		return accent
	}
	return muted
}

func statusText(display repo.DisplayState) string {
	return display.Glyph()
}

func branchColor(display repo.DisplayState) string {
	switch display {
	case repo.DisplayFeature:
		return teal
	case repo.DisplayDetached, repo.DisplayNoUpstream:
		return yellow
	default:
		return blue
	}
}

func colorForState(state repo.DisplayState) string {
	switch state {
	case repo.DisplayClean:
		return green
	case repo.DisplayDirty:
		return yellow
	case repo.DisplayBehind, repo.DisplayError:
		return red
	case repo.DisplayDiverged, repo.DisplayDetached:
		return orange
	case repo.DisplayAhead:
		return green
	case repo.DisplayFeature:
		return teal
	case repo.DisplayNoUpstream:
		return yellow
	default:
		return muted
	}
}

func groupTitle(state repo.DisplayState) string {
	switch state {
	case repo.DisplayClean:
		return "up to date"
	case repo.DisplayDirty:
		return "uncommitted changes"
	case repo.DisplayBehind:
		return "behind remote"
	case repo.DisplayAhead:
		return "ahead of remote"
	case repo.DisplayDiverged:
		return "diverged"
	case repo.DisplayFeature:
		return "feature branch"
	case repo.DisplayDetached:
		return "detached head"
	case repo.DisplayNoUpstream:
		return "no upstream"
	case repo.DisplayError:
		return "error"
	default:
		return "other"
	}
}
