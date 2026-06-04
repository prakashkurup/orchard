package tui

import (
	"math"
	"strings"
	"time"
)

// The launch intro: a small grove of trees grows from the soil, leafs out, then
// blooms into flowers in a slow left-to-right wave, with ORCHARD as a title card
// hugging the treetops. It plays once (~1.9s), then hands off to the dashboard.
// Any key skips it; ORCHARD_NO_ANIM=1 disables it. The motion is frame-driven
// (a normalized 0..1 timeline mapped to screen position), so it always completes
// edge-to-edge in the same wall-clock time at any terminal width.

const (
	introFrames = 74 // ~1.2s at 60fps: long enough to read, short enough to not be in the way
	pulseFrames = 26 // a "just tended" row blossom, ~0.43s
)

const (
	introTrunk = "#7A5C3E" // bark
	introLeaf  = "#3C5A3A" // foliage waiting to bloom
	introGrass = "#2E4A33" // ground line
)

// introPetals is the blossom set, shared with the dashboard "just tended" pulse.
var introPetals = []rune{'✿', '❀', '❁', '✽', '❃', '✾'}

var introBloomCols = []string{brand, orange, yellow, green, teal, cyan, blue, accent}

type introState struct {
	frame int
}

func newIntro(width, height int) *introState { return &introState{} }

// step advances one frame and reports whether the intro is still playing. A hard
// frame cap guarantees it always ends on time, at any width.
func (in *introState) step() bool {
	in.frame++
	return in.frame < introFrames
}

type introCell struct {
	r   rune
	col string
}

type introTree struct {
	cx, trunkH, rx, ry, seed int
	phase                    float64 // 0..1 fraction of the timeline before it wakes
}

func introHash(a, b int) int {
	h := uint32(a)*2654435761 ^ uint32(b)*2246822519
	h ^= h >> 15
	h *= 2654435761
	h ^= h >> 13
	return int(h & 0x7fffffff)
}

func introEase(t float64) float64 {
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	return 1 - math.Pow(1-t, 3)
}

// buildGrove spaces a few trees of fixed modest size across the width (never
// scaled to the terminal, so they don't stretch into lollipops), staggered so
// they wake left-to-right but ALL finish within the timeline. maxH only shrinks
// trees on short terminals.
func buildGrove(w, maxH int) []introTree {
	gap := clamp(w/clamp(w/16, 4, 12), 14, 22)
	var ts []introTree
	var xs []int
	for x := gap / 2; x < w-3; x += gap {
		xs = append(xs, x)
	}
	for i, x := range xs {
		s := introHash(x, 99)
		ry := clamp(2+(s/13)%3, 2, max(2, (maxH-3)/3)) // canopy radius 2-4
		ts = append(ts, introTree{
			cx:     x,
			trunkH: clamp(3+s%4, 2, max(2, maxH-2*ry-1)), // trunk 3-6 rows
			rx:     clamp(3+(s/7)%4, 3, max(3, gap/2-1)),
			ry:     ry,
			seed:   s,
			phase:  float64(i) / float64(max(1, len(xs))) * 0.28,
		})
	}
	return ts
}

func introSet(g [][]introCell, w, x, y int, r rune, col string) {
	if x < 0 || x >= w || y < 0 || y >= len(g) {
		return
	}
	g[y][x] = introCell{r, col}
}

func drawTree(g [][]introCell, w, baseY int, t introTree, growP, leafP, bloomP float64) {
	th := int(math.Round(growP * float64(t.trunkH)))
	if th < 1 && growP > 0 {
		th = 1
	}
	for d := 0; d < th; d++ {
		introSet(g, w, t.cx, baseY-1-d, '┃', introTrunk)
	}
	if th < t.trunkH { // canopy waits until the trunk has fully grown up
		if th > 0 {
			introSet(g, w, t.cx, baseY-1-(th-1), '╿', green) // a green growing tip
		}
		return
	}
	if leafP <= 0 {
		return
	}
	top := baseY - 1 - (t.trunkH - 1)
	introSet(g, w, t.cx-1, top, '╲', introTrunk) // branch shoulders into the canopy
	introSet(g, w, t.cx+1, top, '╱', introTrunk)
	ccy := top - t.ry
	for dy := -t.ry; dy <= t.ry; dy++ {
		for dx := -t.rx; dx <= t.rx; dx++ {
			nx := float64(dx) / (float64(t.rx) + 0.5)
			ny := float64(dy) / (float64(t.ry) + 0.5)
			if nx*nx+ny*ny > 1 {
				continue
			}
			// leaf out from the bottom (the trunk) upward, so nothing ever floats
			bottom := float64(t.ry-dy) / (2*float64(t.ry) + 0.001) // 0 bottom .. 1 top
			if leafP <= bottom {
				continue
			}
			x, y := t.cx+dx, ccy+dy
			hh := introHash(x*3+t.seed, y*7+t.seed)
			openAt := float64(hh%1000) / 1000.0 // each flower opens at its own moment
			if bloomP > openAt {
				introSet(g, w, x, y, introPetals[hh%len(introPetals)], introBloomCols[(hh/6)%len(introBloomCols)])
			} else {
				introSet(g, w, x, y, '❖', introLeaf) // foliage, waiting to bloom
			}
		}
	}
}

func (in *introState) view(width, height int) string {
	if width < 1 || height < 1 {
		return ""
	}
	rows := height
	groundY := rows - 1
	g := float64(in.frame) / float64(introFrames)
	if g > 1 {
		g = 1
	}

	grid := make([][]introCell, rows)
	for y := range grid {
		grid[y] = make([]introCell, width)
		for x := range grid[y] {
			grid[y][x] = introCell{' ', bg}
		}
	}
	for x := 0; x < width; x++ { // ground line
		grid[groundY][x] = introCell{'▁', introGrass}
	}

	trees := buildGrove(width, clamp(groundY-3, 7, 16)) // capped size; only shrinks when short
	titleY := groundY
	for _, t := range trees {
		localT := (g - t.phase) / 0.66              // each tree's own 0..1 window
		growP := introEase(localT / 0.28)           // trunk first
		leafP := introEase((localT - 0.26) / 0.40)  // then leaf out, bottom-up
		bloomP := introEase((localT - 0.52) / 0.48) // then flowers open
		drawTree(grid, width, groundY, t, growP, leafP, bloomP)
		if top := groundY - t.trunkH - 2*t.ry; top < titleY {
			titleY = top // remember the tallest canopy so the title can hug it
		}
	}

	// ORCHARD as a title card hugging just above the treetops, rising in once the
	// grove is in bloom and settling on the current season's colour.
	titleY = clamp(titleY-2, 0, rows-1)
	word := "O R C H A R D"
	wordStart := (width - len(word)) / 2
	if g > 0.58 && wordStart >= 0 {
		ramp := []string{muted, blue, teal, seasonColor(currentSeason(time.Now(), southernHemisphere()))}
		ri := int(introEase((g-0.58)/0.42) * float64(len(ramp)))
		wc := ramp[clamp(ri, 0, len(ramp)-1)]
		for i, r := range word {
			if r != ' ' {
				introSet(grid, width, wordStart+i, titleY, r, wc)
			}
		}
	}

	lines := make([]string, rows)
	for y := 0; y < rows; y++ {
		var b strings.Builder
		gap := 0
		flush := func() {
			if gap > 0 {
				b.WriteString(seg(bg, strings.Repeat(" ", gap)))
				gap = 0
			}
		}
		for x := 0; x < width; x++ {
			c := grid[y][x]
			if c.r == ' ' {
				gap++
				continue
			}
			flush()
			b.WriteString(seg(c.col, string(c.r)))
		}
		flush()
		lines[y] = fillLine(b.String(), width, bg)
	}
	return strings.Join(lines, "\n")
}
