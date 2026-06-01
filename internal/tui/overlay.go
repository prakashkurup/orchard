package tui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var sgrTrueColor = regexp.MustCompile(`(38|48);2;(\d{1,3});(\d{1,3});(\d{1,3})`)

// dimANSI darkens every 24-bit colour in a rendered string to num/den of its
// value, so a backdrop recedes behind a floating modal while staying legible.
func dimANSI(s string, num, den int) string {
	return sgrTrueColor.ReplaceAllStringFunc(s, func(seq string) string {
		mm := sgrTrueColor.FindStringSubmatch(seq)
		scale := func(x string) int {
			v, _ := strconv.Atoi(x)
			return clamp(v*num/den, 0, 255)
		}
		return fmt.Sprintf("%s;2;%d;%d;%d", mm[1], scale(mm[2]), scale(mm[3]), scale(mm[4]))
	})
}

const (
	backdropNum = 55 // dim the dashboard backdrop to 55% brightness behind modals
	backdropDen = 100
)

// dimBg is the app background, dimmed to the same factor used for the backdrop,
// for padding any gaps so they match the recessed dashboard rather than showing
// the terminal's default colour. Derived from the bg constant so it tracks the
// palette.
var dimBg = dimHex(bg, backdropNum, backdropDen)

// dimHex scales a "#rrggbb" colour to num/den of its brightness.
func dimHex(hex string, num, den int) string {
	r, g, b := hexRGB(hex)
	return fmt.Sprintf("#%02X%02X%02X", r*num/den, g*num/den, b*num/den)
}

// overlayCenter composites box centred over backdrop, returning exactly
// height rows each width cells wide.
func overlayCenter(box, backdrop string, width, height int) string {
	fillStyle := lipgloss.NewStyle().Background(lipgloss.Color(dimBg))
	fill := func(n int) string {
		if n <= 0 {
			return ""
		}
		return fillStyle.Render(strings.Repeat(" ", n))
	}
	// normalise a backdrop row to exactly `width` cells.
	norm := func(s string) string {
		w := ansi.StringWidth(s)
		switch {
		case w > width:
			return ansi.Truncate(s, width, "")
		case w < width:
			return s + fill(width-w)
		default:
			return s
		}
	}

	rows := strings.Split(backdrop, "\n")
	for len(rows) < height {
		rows = append(rows, "")
	}
	rows = rows[:height]
	for i := range rows {
		rows[i] = norm(rows[i])
	}

	boxLines := strings.Split(box, "\n")
	bw := 0
	for _, l := range boxLines {
		if w := ansi.StringWidth(l); w > bw {
			bw = w
		}
	}
	x := max(0, (width-bw)/2)
	y := max(0, (height-len(boxLines))/2)

	for i, bl := range boxLines {
		row := y + i
		if row < 0 || row >= height {
			continue
		}
		base := rows[row]
		left := ansi.Truncate(base, x, "")
		if w := ansi.StringWidth(left); w < x {
			left += fill(x - w)
		}
		right := ansi.TruncateLeft(base, x+bw, "")
		rows[row] = left + bl + right
	}
	return strings.Join(rows, "\n")
}

// overlayModal floats a modal box over the dimmed dashboard so there is context
// behind it instead of a dark void with letterbox bands above and below.
func (m model) overlayModal(box string, inner int) string {
	// dim the view the modal was opened over (the detail page when it came from
	// there, otherwise the dashboard) so closing returns you to the same place
	base := m.dashboardBody(inner)
	if m.returnMode == modeDetail && m.detail != nil {
		base = m.detailView(inner)
	}
	backdrop := dimANSI(base, backdropNum, backdropDen)
	return overlayCenter(box, backdrop, inner, max(1, m.height-2))
}
