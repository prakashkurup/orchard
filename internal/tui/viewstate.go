package tui

import (
	"fmt"
	"github.com/prakashkurup/orchard/internal/repo"
	"sort"
	"strings"
	"time"
)

// aiTouchedWindow: how recent a Claude run counts for the ai-touched filter.
const aiTouchedWindow = 7 * 24 * time.Hour

func (m *model) toggleCurrent() {
	r, ok := m.currentRepo()
	if !ok {
		return
	}
	if m.selected[r.Path] {
		delete(m.selected, r.Path)
		m.status = "deselected " + r.Name
	} else {
		m.selected[r.Path] = true
		m.status = "selected " + r.Name
	}
}

func (m *model) selectAllVisible() {
	n := 0
	for _, it := range m.view {
		if it.header {
			continue
		}
		m.selected[m.repos[it.repoIdx].Path] = true
		n++
	}
	m.status = fmt.Sprintf("selected %d repos", n)
}

func (m *model) rebuildView() {
	idxs := make([]int, 0, len(m.repos))
	for i, r := range m.repos {
		if !m.passQuick(r) {
			continue
		}
		if m.filterText != "" && !repoMatches(r, m.filterText) {
			continue
		}
		idxs = append(idxs, i)
	}
	m.sortIndices(idxs)

	m.view = m.view[:0]
	if m.grouped {
		// stable: idxs already sorted; bucket by display state in attention order
		for _, st := range groupOrder() {
			members := make([]int, 0)
			for _, i := range idxs {
				if m.repos[i].Display == st {
					members = append(members, i)
				}
			}
			if len(members) == 0 {
				continue
			}
			m.view = append(m.view, viewItem{header: true, group: st, count: len(members)})
			for _, i := range members {
				m.view = append(m.view, viewItem{repoIdx: i})
			}
		}
	} else {
		for _, i := range idxs {
			m.view = append(m.view, viewItem{repoIdx: i})
		}
	}
	m.normalizeCursor()
}

func (m *model) sortIndices(idxs []int) {
	sort.SliceStable(idxs, func(a, b int) bool {
		ra, rb := m.repos[idxs[a]], m.repos[idxs[b]]
		switch m.sortMode {
		case sortName:
			return strings.ToLower(ra.Name) < strings.ToLower(rb.Name)
		case sortSynced:
			return ra.LastFetched.Before(rb.LastFetched)
		case sortClaude:
			if ra.CCLast.Equal(rb.CCLast) {
				return strings.ToLower(ra.Name) < strings.ToLower(rb.Name)
			}
			return ra.CCLast.After(rb.CCLast) // most recently Claude-worked first
		default: // attention
			pa, pb := attentionRank(ra.Display), attentionRank(rb.Display)
			if pa != pb {
				return pa < pb
			}
			return strings.ToLower(ra.Name) < strings.ToLower(rb.Name)
		}
	})
}

func (m model) passQuick(r repo.Repo) bool {
	switch m.quick {
	case filterAttention:
		return r.Display != repo.DisplayClean
	case filterDirty:
		return r.Display == repo.DisplayDirty
	case filterBehind:
		return r.Display == repo.DisplayBehind || r.Display == repo.DisplayDiverged
	case filterFeature:
		return r.Display == repo.DisplayFeature
	case filterRisk:
		return r.Dirty || r.Ahead > 0 || r.Stashes > 0
	case filterAITouched:
		return !r.CCLast.IsZero() && time.Since(r.CCLast) <= aiTouchedWindow
	case filterNeedsInstr:
		s, ok := m.instructionsByPath[r.Path]
		return ok && s.blind() // only once instruction health is known (avoids startup flicker)
	default:
		return true
	}
}

// repoMatches is the `/` filter: bare = name or branch; branch:/name: scope it.
func repoMatches(r repo.Repo, q string) bool {
	q = strings.ToLower(strings.TrimSpace(q))
	switch {
	case strings.HasPrefix(q, "branch:"):
		return strings.Contains(strings.ToLower(r.Branch), strings.TrimPrefix(q, "branch:"))
	case strings.HasPrefix(q, "name:"):
		return strings.Contains(strings.ToLower(r.Name), strings.TrimPrefix(q, "name:"))
	default:
		return strings.Contains(strings.ToLower(r.Name), q) ||
			strings.Contains(strings.ToLower(r.Branch), q)
	}
}

// scrollActive scrolls whatever cursor-based view is in front by delta rows
// (negative = up), so the mouse wheel works everywhere. The detail-viewport
// pagers are eased separately via easeScrollBy.
func (m *model) scrollActive(delta int) {
	switch m.mode {
	case modeList:
		m.moveCursor(delta)
		m.syncRows()
	case modeSearch:
		if len(m.searchFlat) > 0 {
			m.searchCursor = clamp(m.searchCursor+delta, 0, len(m.searchFlat)-1)
			m.setSearchContent()
		}
	case modeSessions:
		m.sessionCursor = clamp(m.sessionCursor+delta, 0, max(0, len(m.sessions)-1))
	case modeSessionSearch:
		m.sessionSearchCursor = clamp(m.sessionSearchCursor+delta, 0, max(0, len(m.sessionSearchResults)-1))
	case modePresets:
		if !m.presetNaming {
			m.presetCursor = clamp(m.presetCursor+delta, 0, max(0, len(sortedPresetNames(m.presets))-1))
		}
	case modeTouched:
		m.touchedCursor = clamp(m.touchedCursor+delta, 0, max(0, len(m.touchedFiles)-1))
	}
}

// rowAtY maps a mouse Y to a repo row index in m.view, or -1 for a header or
// out-of-range click. Mouse coordinates include appStyle's top padding, then
// header (3), metrics (1), and grid header (1), so repo content starts at line 6.
func (m *model) rowAtY(y int) int {
	const firstRepoRowY = 6
	idx := m.viewport.YOffset + (y - firstRepoRowY)
	if idx < 0 || idx >= len(m.view) || m.view[idx].header {
		return -1
	}
	return idx
}

// clickToRow focuses the repo row under a mouse click and toggles its selection.
func (m *model) clickToRow(y int) {
	idx := m.rowAtY(y)
	if idx < 0 {
		return
	}
	m.cursor = idx
	m.ensureCursorVisible()
	m.toggleCurrent()
}

func (m *model) normalizeCursor() {
	if len(m.view) == 0 {
		m.cursor = 0
		return
	}
	m.cursor = clamp(m.cursor, 0, len(m.view)-1)
	m.cursor = m.snapToRepo(m.cursor, 1)
	m.ensureCursorVisible()
}

func (m *model) snapToRepo(pos, dir int) int {
	if dir == 0 {
		dir = 1
	}
	for p := pos; p >= 0 && p < len(m.view); p += dir {
		if !m.view[p].header {
			return p
		}
	}
	for p := pos; p >= 0 && p < len(m.view); p -= dir {
		if !m.view[p].header {
			return p
		}
	}
	return pos
}

func (m *model) moveCursor(delta int) {
	if len(m.view) == 0 {
		m.cursor = 0
		return
	}
	pos := clamp(m.cursor+delta, 0, len(m.view)-1)
	m.cursor = m.snapToRepo(pos, sign(delta))
	m.ensureCursorVisible()
}

func (m *model) cursorToEdge(top bool) {
	if len(m.view) == 0 {
		return
	}
	if top {
		m.cursor = m.snapToRepo(0, 1)
	} else {
		m.cursor = m.snapToRepo(len(m.view)-1, -1)
	}
	m.ensureCursorVisible()
}

func (m model) currentRepo() (repo.Repo, bool) {
	if m.cursor < 0 || m.cursor >= len(m.view) {
		return repo.Repo{}, false
	}
	it := m.view[m.cursor]
	if it.header {
		return repo.Repo{}, false
	}
	return m.repos[it.repoIdx], true
}

func (m model) repoByPath(path string) repo.Repo {
	for _, r := range m.repos {
		if r.Path == path {
			return r
		}
	}
	return repo.Repo{}
}

func (m model) pullTargets() []repo.Repo {
	if len(m.selected) == 0 {
		if r, ok := m.currentRepo(); ok {
			return []repo.Repo{r}
		}
		return nil
	}
	targets := make([]repo.Repo, 0, len(m.selected))
	for _, r := range m.repos {
		if m.selected[r.Path] {
			targets = append(targets, r)
		}
	}
	return targets
}

func (m model) eligibleAll() []repo.Repo {
	out := make([]repo.Repo, 0, len(m.view))
	for _, it := range m.view {
		if it.header {
			continue
		}
		out = append(out, m.repos[it.repoIdx])
	}
	return out
}

func (m *model) dropMissingSelections() {
	known := map[string]bool{}
	for _, r := range m.repos {
		known[r.Path] = true
	}
	for path := range m.selected {
		if !known[path] {
			delete(m.selected, path)
		}
	}
}

func (m *model) ensureCursorVisible() {
	if m.cursor < m.viewport.YOffset {
		m.viewport.SetYOffset(m.cursor)
	}
	bottom := m.viewport.YOffset + max(1, m.viewport.Height)
	if m.cursor >= bottom {
		m.viewport.SetYOffset(m.cursor - max(1, m.viewport.Height) + 1)
	}
}

func (m *model) jumpToNextNew() {
	if len(m.view) == 0 {
		return
	}
	n := len(m.view)
	for off := 1; off <= n; off++ {
		i := (m.cursor + off) % n
		it := m.view[i]
		if !it.header && m.newByPath[m.repos[it.repoIdx].Path] > 0 {
			m.cursor = i
			m.ensureCursorVisible()
			return
		}
	}
	m.status = "no repos with new commits"
}

func groupOrder() []repo.DisplayState {
	return []repo.DisplayState{
		repo.DisplayError,
		repo.DisplayDiverged,
		repo.DisplayBehind,
		repo.DisplayDirty,
		repo.DisplayDetached,
		repo.DisplayFeature,
		repo.DisplayAhead,
		repo.DisplayNoUpstream,
		repo.DisplayClean,
	}
}

func attentionRank(state repo.DisplayState) int {
	for i, s := range groupOrder() {
		if s == state {
			return i
		}
	}
	return 99
}

func (m model) selectedCount() int {
	return len(m.selected)
}
