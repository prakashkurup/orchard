package tui

import (
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/prakashkurup/orchard/internal/repo"
)

// The Konami code (↑ ↑ ↓ ↓ ← → ← →) on the dashboard makes the orchard
// blossom for a moment.
type bloomTickMsg struct{}

const bloomFrames = 16 // ~1.3s at 80ms/frame

var konamiSeq = []string{"up", "up", "down", "down", "left", "right", "left", "right"}

func bloomTickCmd() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(time.Time) tea.Msg { return bloomTickMsg{} })
}

// pushKonami records an arrow key and reports whether the full sequence just
// completed. A non-arrow key resets the buffer.
func (m *model) pushKonami(key string) bool {
	switch key {
	case "up", "down", "left", "right":
		m.konami = append(m.konami, key)
		if len(m.konami) > len(konamiSeq) {
			m.konami = m.konami[len(m.konami)-len(konamiSeq):]
		}
	default:
		m.konami = nil
		return false
	}
	if len(m.konami) != len(konamiSeq) {
		return false
	}
	for i := range konamiSeq {
		if m.konami[i] != konamiSeq[i] {
			return false
		}
	}
	m.konami = nil
	return true
}

// bloom renders a width-wide band of drifting blossoms for the header rule line
// during the bloom animation. frame advances each tick so it shimmers.
func bloom(width, frame int) string {
	petals := []string{"✿", "❀", "❁", "✽", "❃"}
	colors := []string{accent, brand, blue, green, teal, yellow, orange}
	var b strings.Builder
	for x := 0; x < width; x++ {
		if (x+frame)%4 == 0 {
			b.WriteString(seg(colors[(x+frame)%len(colors)], petals[(x/4+frame)%len(petals)]))
		} else {
			b.WriteString(seg(muted, "─"))
		}
	}
	return b.String()
}

// isArborDay reports whether t falls on National Arbor Day (the last Friday of
// April). On that day the wordmark sprouts a little seedling. Set ORCHARD_ARBOR=1
// to preview it on any date.
func isArborDay(t time.Time) bool {
	if v := os.Getenv("ORCHARD_ARBOR"); v == "1" || strings.EqualFold(v, "true") {
		return true
	}
	if t.Month() != time.April {
		return false
	}
	last := time.Date(t.Year(), time.May, 0, 0, 0, 0, 0, t.Location()) // April 30
	for last.Weekday() != time.Friday {
		last = last.AddDate(0, 0, -1)
	}
	return t.Day() == last.Day()
}

// tendingLines are the orchard-flavored "working" messages shown while the
// dashboard scans or refreshes, in place of a plain "loading".
var tendingLines = []string{
	"tending the trees…",
	"checking for ripe fruit…",
	"pruning dead branches…",
	"counting rings…",
	"watering the roots…",
	"walking the rows…",
}

func tendingLine() string {
	return tendingLines[rand.Intn(len(tendingLines))]
}

// farewells are printed once, quietly, after you leave the dashboard.
var farewells = []string{
	"the orchard rests.",
	"see you in the rows.",
	"leave the gate as you found it.",
	"mind the saplings on your way out.",
	"the trees will be here when you return.",
}

func farewell() string {
	return farewells[rand.Intn(len(farewells))]
}

// isThriving reports whether every repo is healthy: clean, or merely ahead of
// its remote or on a feature branch with a clean tree. Used for the small
// "thriving" reward in the metrics row.
func (m model) isThriving() bool {
	if len(m.repos) == 0 {
		return false
	}
	for _, r := range m.repos {
		switch r.Display {
		case repo.DisplayClean, repo.DisplayAhead, repo.DisplayFeature:
			// healthy
		default:
			return false
		}
	}
	return true
}

// season is the time of year, which sets the orchard's mood glyph. The orchard
// follows the seasons like a quiet rural-life game: spring blossoms, summer
// leaf, autumn fall, winter snow, shown as a single tinted glyph by the wordmark.
type season int

const (
	spring season = iota
	summer
	autumn
	winter
)

// currentSeason maps a date to a meteorological season (seasons turn on the 1st
// of Mar/Jun/Sep/Dec), flipped for the southern hemisphere. Set ORCHARD_SEASON
// to spring/summer/autumn/winter to preview a season's wordmark color on any date.
func currentSeason(t time.Time, southern bool) season {
	switch strings.ToLower(os.Getenv("ORCHARD_SEASON")) {
	case "spring":
		return spring
	case "summer":
		return summer
	case "autumn", "fall":
		return autumn
	case "winter":
		return winter
	}
	var s season
	switch t.Month() {
	case time.March, time.April, time.May:
		s = spring
	case time.June, time.July, time.August:
		s = summer
	case time.September, time.October, time.November:
		s = autumn
	default:
		s = winter
	}
	if southern {
		s = (s + 2) % 4
	}
	return s
}

// seasonColor is the ORCHARD wordmark tint for a season: the logo shifts color
// through the year (candy-red spring, lush-green summer, amber autumn, icy
// winter). Tinting plain-ASCII letters avoids the alignment issues that a
// separate ambiguous-width glyph caused.
func seasonColor(s season) string {
	switch s {
	case spring:
		return brand
	case summer:
		return green
	case autumn:
		return orange
	default:
		return cyan
	}
}

// southernHemisphere best-effort detects the southern hemisphere, fully offline:
// the ORCHARD_HEMISPHERE override wins, otherwise the system timezone name is
// matched against known southern regions. Defaults to northern.
func southernHemisphere() bool {
	switch strings.ToLower(os.Getenv("ORCHARD_HEMISPHERE")) {
	case "south", "southern", "s":
		return true
	case "north", "northern", "n":
		return false
	}
	tz := localZoneName()
	south := []string{
		"Australia/", "Antarctica/", "Pacific/Auckland", "Pacific/Chatham",
		"Pacific/Fiji", "Pacific/Port_Moresby", "America/Argentina/",
		"America/Santiago", "America/Sao_Paulo", "America/Montevideo",
		"America/Asuncion", "America/La_Paz", "America/Lima",
		"Africa/Johannesburg", "Africa/Windhoek", "Africa/Maputo", "Africa/Harare",
	}
	for _, p := range south {
		if strings.HasPrefix(tz, p) {
			return true
		}
	}
	return false
}

// localZoneName returns the IANA timezone name (e.g. "Asia/Tokyo") without any
// network call: the TZ env var, else the /etc/localtime symlink target.
func localZoneName() string {
	if tz := os.Getenv("TZ"); tz != "" {
		return tz
	}
	if p, err := os.Readlink("/etc/localtime"); err == nil {
		if i := strings.LastIndex(p, "zoneinfo/"); i >= 0 {
			return p[i+len("zoneinfo/"):]
		}
	}
	return ""
}

// Idle screensaver: after idleAfter seconds with no input on the list, the
// orchard "sleeps" and petals drift down the screen until any key is pressed.
const (
	idleProbe  = time.Second            // idle-detection cadence while awake
	ssFrameDur = 280 * time.Millisecond // animation cadence while sleeping
)

// idleSeconds is how long the dashboard sits idle before the screensaver wakes,
// from ORCHARD_IDLE_SECS (default 600, i.e. 10 minutes). 0 or negative disables
// it; the Z key always starts it on demand regardless.
func idleSeconds() int {
	if v := os.Getenv("ORCHARD_IDLE_SECS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return 600
}

// fetchIntervalSecs is how often the dashboard fetches remotes in the background
// while live refresh is on, from ORCHARD_FETCH_SECS (default 300 = 5 minutes).
// 0 or negative disables background fetching (live refresh still re-reads local
// git state every refreshInterval; you can always fetch on demand with f / F).
func fetchIntervalSecs() int {
	if v := os.Getenv("ORCHARD_FETCH_SECS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return 300
}

// screensaverView renders one frame of the idle animation: petals falling down
// a dark field, with a faint centered hint. Every cell is painted on the app
// background so there is no banding.
func screensaverView(width, height, frame int) string {
	if width < 1 || height < 1 {
		return ""
	}
	petals := []string{"✿", "❀", "❁", "❃", "✽"}
	// a full neon spectrum (warm to cool), for a vivid multicolor drift
	cols := []string{brand, orange, yellow, green, teal, cyan, blue, accent}
	rows := make([]string, height)
	for y := 0; y < height; y++ {
		var b strings.Builder
		gap := 0
		flush := func() {
			if gap > 0 {
				b.WriteString(seg(bg, strings.Repeat(" ", gap)))
				gap = 0
			}
		}
		for x := 0; x < width; x++ {
			if h, ok := petalAt(x, y, frame); ok {
				flush()
				b.WriteString(seg(cols[h%len(cols)], petals[(h/7)%len(petals)]))
			} else {
				gap++
			}
		}
		flush()
		rows[y] = fillLine(b.String(), width, bg)
	}
	if height >= 3 {
		hint := "the orchard sleeps   ·   press any key"
		if w := len([]rune(hint)); w < width {
			pad := (width - w) / 2
			rows[height/2] = fillLine(seg(muted, strings.Repeat(" ", pad)+hint), width, bg)
		}
	}
	return strings.Join(rows, "\n")
}

// petalAt reports whether a petal sits at (x, y) on the given frame, and a stable
// hash for it (so its glyph and color stay constant as it falls). The lattice is
// keyed by (y - frame), so the whole field scrolls down one row per frame.
func petalAt(x, y, frame int) (int, bool) {
	h := x*73856093 ^ (y-frame)*19349663
	if h < 0 {
		h = -h
	}
	return h, h%27 == 0
}

// groveLabel is a playful collective noun once you are tending enough repos.
func groveLabel(n int) string {
	switch {
	case n >= 100:
		return "an orchard"
	case n >= 25:
		return "a grove"
	default:
		return ""
	}
}
