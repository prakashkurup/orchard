// Package tui is the Bubble Tea dashboard for managing many local git repos.
package tui

import (
	"context"
	"fmt"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/prakashkurup/orchard/internal/claude"
	"github.com/prakashkurup/orchard/internal/editor"
	orchardgit "github.com/prakashkurup/orchard/internal/git"
	"github.com/prakashkurup/orchard/internal/github"
	"github.com/prakashkurup/orchard/internal/lang"
	"github.com/prakashkurup/orchard/internal/repo"
	"github.com/prakashkurup/orchard/internal/search"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type uiMode int

const (
	modeList uiMode = iota
	modeDetail
	modeEditor
	modeSearch
	modeBranch
	modeHelp
	modeWorklog
	modeClone
	modeConfirm
	modeSessions
	modeDiff
	modeStats
	modeCommitMsg
	modeSessionSearch
	modePresets
	modeTouched
	modePreview
)

type sortMode int

const (
	sortAttention sortMode = iota
	sortName
	sortSynced
	sortClaude
	sortModeCount
)

func (s sortMode) String() string {
	switch s {
	case sortName:
		return "name"
	case sortSynced:
		return "synced"
	case sortClaude:
		return "claude"
	default:
		return "attention"
	}
}

type quickFilter int

const (
	filterAll quickFilter = iota
	filterAttention
	filterDirty
	filterBehind
	filterFeature
	filterRisk       // work at risk: uncommitted, unpushed, or stashed
	filterAITouched  // Claude ran here recently
	filterNeedsInstr // has AGENTS.md but Claude won't read it
	filterCount      // sentinel: number of quick filters
)

func (q quickFilter) String() string {
	switch q {
	case filterAttention:
		return "attention"
	case filterDirty:
		return "dirty"
	case filterBehind:
		return "behind"
	case filterFeature:
		return "feature"
	case filterRisk:
		return "at-risk"
	case filterAITouched:
		return "ai-touched"
	case filterNeedsInstr:
		return "needs-md"
	default:
		return "all"
	}
}

// viewItem is one rendered line: either a group header or a repo row.
type viewItem struct {
	header  bool
	group   repo.DisplayState
	count   int
	repoIdx int // index into m.repos
}

type model struct {
	root        string
	concurrency int
	width       int
	height      int
	cursor      int // index into m.view, always on a repo row

	viewport    viewport.Model
	detailVP    viewport.Model
	spinner     spinner.Model
	filterInput textinput.Model

	repos    []repo.Repo
	selected map[string]bool
	view     []viewItem

	mode       uiMode
	sortMode   sortMode
	grouped    bool
	quick      quickFilter
	filtering  bool
	filterText string

	pulling                      map[string]bool
	pullDone, pullSkip, pullFail int

	detail     *detailState
	detailRepo string

	editorID     string
	editorPick   []editor.Editor
	editorCursor int
	editorRepo   string

	branchRepo    string
	branchInput   textinput.Model
	branchAll     []orchardgit.Branch
	branchCursor  int
	branchLoading bool
	branchBusy    bool   // a checkout is in flight (modal stays open)
	branchTarget  string // branch being switched to (for retry / message)
	branchErr     string // checkout error shown in the modal, "" when none

	cloneInput textinput.Model

	confirmRepos []repo.Repo // pending targets awaiting confirmation
	confirmKind  confirmKind // which action the confirmation will run
	confirmYes   bool        // confirm modal selection (true = Yes, the default)

	claudeUsage *claude.Usage

	sessionsRepo    repo.Repo // repo whose Claude Code sessions are being browsed
	sessions        []claude.Session
	sessionCursor   int
	sessionsLoading bool
	sessionsErr     string

	ghStatus map[string]github.RepoStatus // repo path -> open PRs + CI state

	diffRepo repo.Repo // repo whose working-tree diff is shown
	diffText string    // raw diff text, kept so it can re-colorize on resize
	diffPath string    // single file the diff is scoped to ("" = whole working tree)

	touchedRepo    repo.Repo            // repo whose touched-files list is shown
	touchedFiles   []claude.TouchedFile // files Claude read/edited there
	touchedDirty   map[string]bool      // repo-relative paths with uncommitted changes
	touchedCursor  int
	touchedLoading bool
	touchedReturn  uiMode // where esc returns (own field, since opening a diff reuses returnMode)

	previewRepo  repo.Repo // repo whose markdown docs are previewed
	previewDocs  []string  // the md files that exist (CLAUDE.md / AGENTS.md / README.md)
	previewIdx   int       // which doc is shown
	previewBytes int       // size of the shown doc, for the est-tokens readout

	returnMode uiMode // where a modal returns to on close (list, or detail if opened there)

	statsLoading bool           // the stats heatmaps are still computing
	statsHarvest map[string]int // commit day -> count (stats page)
	statsClaude  map[string]int // Claude session day -> turns (stats page)

	commitMsgRepo    repo.Repo // repo a headless commit message is being drafted for
	commitMsg        string    // the drafted message
	commitMsgErr     string    // draft failure, if any
	commitMsgLoading bool      // claude -p is still running
	commitMsgCopied  bool      // message was copied to the clipboard
	commitMsgFrame   int       // animation frame for the drafting indicator

	sessionSearchInput   textinput.Model
	sessionSearchResults []claude.SessionHit
	sessionSearchCursor  int
	sessionSearchQuery   string
	sessionSearchFocus   bool // true = editing the query, false = navigating results
	sessionSearchRunning bool

	searchInput   textinput.Model
	searchVP      viewport.Model
	searchResults []search.Result
	searchFlat    []search.Match
	searchCursor  int
	searchQuery   string
	searchFocus   bool // true = editing query, false = navigating results
	searchRunning bool

	autoRefresh bool
	bgFetching  bool // a background fetch is in flight (avoids overlapping fetches)

	worklogWindow string // git --since value, e.g. "1 day ago"
	worklogText   string // plain-text digest for clipboard copy

	newByPath          map[string]int        // repo path -> commits since last visit
	langByPath         map[string]lang.Stat  // repo path -> dominant language
	instructionsByPath map[string]instrState // repo path -> CLAUDE.md / AGENTS.md health
	seenChecked        bool

	konami      []string // recent arrow keys, for the bloom easter egg
	bloomFrames int      // remaining frames of the bloom animation

	// one-shot spring animations (harmonica). The 60fps ticker runs only while
	// something is actually moving, then stops, so the idle dashboard is still.
	animOn     bool
	introShown bool           // the launch intro has played (once per process)
	intro      *introState    // non-nil while the launch intro is on screen
	repoCount  spring1d       // animated REPOS metric (counts up after a scan)
	pulses     map[string]int // repo path -> remaining frames of a "just tended" pulse

	// one-time top-to-bottom row reveal when the dashboard first appears
	revealActive bool
	revealFrame  int
	revealLines  int
	revealed     bool // guard: the cascade only ever plays once per process

	idle      int  // idle-probe ticks since the last input (≈ seconds awake)
	idleAfter int  // seconds of idle before the screensaver wakes (0 = disabled)
	idleGen   int  // generation token so a fresh idle tick supersedes stale ones
	ssActive  bool // the idle screensaver is showing
	ssFrame   int  // screensaver animation frame

	assistantCmd   string // AI assistant launched by `c` ("" = none found)
	assistantLabel string // short footer label for the assistant

	version   string // running version, for the update check
	updateTag string // a newer release tag if one is available ("" = up to date)

	presets      map[string][]string // named repo sets, for one-key cross-repo launches
	presetCursor int
	presetNaming bool // true = typing a name for a new preset
	presetInput  textinput.Model

	loading bool
	status  string
	err     string
}

type scanMsg struct {
	repos []repo.Repo
	err   error
}

type pullOneMsg struct {
	result orchardgit.PullResult
}

type fetchOneMsg struct {
	repo repo.Repo
	err  error
}

type statusMsg struct {
	text string
}

type detailMsg struct {
	path         string
	info         orchardgit.DetailInfo
	langs        []lang.Stat
	sessions     []claude.Session
	commitsSince int
	touched      []claude.TouchedFile
	err          error
}

type claudeStatsMsg struct {
	usage claude.Usage
}

type tickMsg time.Time

// fetchTickMsg fires the background-fetch cadence (while live refresh is on).
type fetchTickMsg time.Time

// bgFetchMsg carries the repos after a background fetch + rescan.
type bgFetchMsg struct{ repos []repo.Repo }

// idleTickMsg drives idle detection and the screensaver; gen is matched against
// the model's current idleGen so superseded ticks are ignored.
type idleTickMsg struct{ gen int }

// newCommitsMsg carries the "since last visit" counts computed once at startup.
type newCommitsMsg struct {
	byPath map[string]int
}

// langMsg carries dominant languages computed once at startup.
type langMsg struct {
	byPath map[string]lang.Stat
}

type worklogGroup struct {
	repo    string
	commits []orchardgit.Commit
}

type worklogMsg struct {
	window string
	groups []worklogGroup
	total  int
	text   string // plain text for clipboard
}

type cloneDoneMsg struct {
	name string
	err  error
}

// silentScanMsg is the result of a background auto-refresh (no loading flicker,
// no Claude re-aggregation).
type silentScanMsg struct {
	repos []repo.Repo
}

type branchesMsg struct {
	path     string
	branches []orchardgit.Branch
	err      error
}

type checkoutMsg struct {
	repo    repo.Repo
	branch  string
	err     error
	stashed bool // true when the switch was preceded by an auto-stash
}

type searchResultMsg struct {
	query   string
	results []search.Result
}

func Run(root string, concurrency int, version string) error {
	m := newModel(root, concurrency)
	m.version = version
	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if os.Getenv("ORCHARD_NO_MOUSE") == "" {
		opts = append(opts, tea.WithMouseCellMotion()) // click to focus a row, wheel to scroll
	}
	_, err := tea.NewProgram(m, opts...).Run()
	if err == nil {
		fmt.Println(farewell())
	}
	return err
}

func Preview(root string, concurrency, width, height int, grouped bool) (string, error) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	if demoMode() {
		m := newModel(root, concurrency)
		m.width, m.height = width, height
		m.loading = false
		m.grouped = grouped
		m.repos = demoRepos()
		m.status = fmt.Sprintf("previewing %d repos", len(m.repos))
		u := demoClaude()
		m.claudeUsage = &u
		m.langByPath = demoLangs()
		m.newByPath = demoNew()
		m.ghStatus = demoGHStatus()
		m.instructionsByPath = demoInstr()
		m.resize()
		m.syncRows()
		return m.View(), nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	repos, err := orchardgit.Scan(ctx, root, concurrency)
	if err != nil {
		return "", err
	}
	enrichClaude(repos)
	m := newModel(root, concurrency)
	m.width = width
	m.height = height
	m.loading = false
	m.grouped = grouped
	m.status = fmt.Sprintf("previewing %d repos", len(repos))
	m.repos = repos
	targets := make([]claude.Target, 0, len(repos))
	for _, r := range repos {
		targets = append(targets, claude.Target{Name: r.Name, Path: r.Path})
	}
	u := claude.Aggregate(targets)
	m.claudeUsage = &u
	for _, r := range repos {
		if s := lang.Dominant(ctx, r.Path); s.Name != "" {
			m.langByPath[r.Path] = s
		}
	}
	m.resize()
	m.syncRows()
	return m.View(), nil
}

// PreviewDetail renders the detail view for one repo (by name) once, for testing.
func PreviewDetail(root string, concurrency, width, height int, name string) (string, error) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var repos []repo.Repo
	if demoMode() {
		repos = demoRepos()
	} else {
		var err error
		repos, err = orchardgit.Scan(ctx, root, concurrency)
		if err != nil {
			return "", err
		}
		enrichClaude(repos)
	}
	m := newModel(root, concurrency)
	m.width, m.height = width, height
	m.loading = false
	m.repos = repos
	m.resize()
	var target repo.Repo
	for _, r := range repos {
		if r.Name == name {
			target = r
		}
	}
	if target.Path == "" {
		return "", fmt.Errorf("repo %q not found", name)
	}
	m.mode = modeDetail
	m.detailRepo = target.Path
	if demoMode() {
		m.ghStatus = demoGHStatus()
		m.instructionsByPath = demoInstr()
		m.detail = &detailState{repo: target, info: demoDetail(target), langs: demoDetailLangs(target.Path), sessions: demoSessions(), commitsSince: 14, touched: demoTouched()}
	} else {
		info, _ := orchardgit.Detail(ctx, target)
		m.instructionsByPath = map[string]instrState{target.Path: detectInstr(target.Path)}
		sessions := claude.Sessions(target.Path, 10)
		m.detail = &detailState{repo: target, info: info, langs: lang.Detect(ctx, target.Path), sessions: sessions, commitsSince: commitsSinceClaude(ctx, target.Path, sessions), touched: claude.TouchMap(target.Path, touchMapSessions)}
	}
	m.setDetailContent()
	return m.View(), nil
}

func newModel(root string, concurrency int) model {
	vp := viewport.New(100, 16)
	vp.MouseWheelEnabled = true

	dvp := viewport.New(100, 16)
	dvp.MouseWheelEnabled = true

	sp := spinner.New()
	sp.Spinner = spinner.MiniDot
	// No style here: spinner.View() must return a bare frame. Color is applied by
	// whichever cell renders it, so the ANSI never gets re-processed by fit().
	sp.Style = lipgloss.NewStyle()

	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "filter by name or branch…"
	ti.CharLimit = 64

	si := textinput.New()
	si.Prompt = ""
	si.Placeholder = "search code across all repos…"
	si.CharLimit = 128

	svp := viewport.New(100, 16)
	svp.MouseWheelEnabled = true

	bi := textinput.New()
	bi.Prompt = ""
	bi.Placeholder = "filter branches…"
	bi.CharLimit = 80

	ci := textinput.New()
	ci.Prompt = ""
	ci.Placeholder = "git URL or owner/repo…"
	ci.CharLimit = 200

	qi := textinput.New()
	qi.Prompt = ""
	qi.Placeholder = "search across all Claude sessions…"
	qi.CharLimit = 200

	pi := textinput.New()
	pi.Prompt = ""
	pi.Placeholder = "preset name…"
	pi.CharLimit = 60

	// Inputs must paint their own background or the placeholder (256-color grey
	// with no bg) falls through to the terminal default and shows as a grey box.
	// Modal inputs sit on the panel; the dashboard filter and search on the app bg.
	onPanel := lipgloss.NewStyle().Background(lipgloss.Color(panel))
	onBg := lipgloss.NewStyle().Background(lipgloss.Color(bg))
	bi.PlaceholderStyle = onPanel.Foreground(lipgloss.Color(muted))
	bi.TextStyle = onPanel.Foreground(lipgloss.Color(ice))
	ci.PlaceholderStyle = onPanel.Foreground(lipgloss.Color(muted))
	ci.TextStyle = onPanel.Foreground(lipgloss.Color(ice))
	pi.PlaceholderStyle = onPanel.Foreground(lipgloss.Color(muted))
	pi.TextStyle = onPanel.Foreground(lipgloss.Color(ice))
	ti.PlaceholderStyle = onBg.Foreground(lipgloss.Color(muted))
	ti.TextStyle = onBg.Foreground(lipgloss.Color(ice))
	si.PlaceholderStyle = onBg.Foreground(lipgloss.Color(muted))
	si.TextStyle = onBg.Foreground(lipgloss.Color(ice))
	qi.PlaceholderStyle = onBg.Foreground(lipgloss.Color(muted))
	qi.TextStyle = onBg.Foreground(lipgloss.Color(ice))

	aCmd, aLabel, _ := resolveAssistant()

	return model{
		root:               root,
		concurrency:        concurrency,
		viewport:           vp,
		detailVP:           dvp,
		spinner:            sp,
		filterInput:        ti,
		searchInput:        si,
		searchVP:           svp,
		branchInput:        bi,
		cloneInput:         ci,
		sessionSearchInput: qi,
		presetInput:        pi,
		presets:            map[string][]string{},
		selected:           map[string]bool{},
		pulling:            map[string]bool{},
		newByPath:          map[string]int{},
		langByPath:         map[string]lang.Stat{},
		pulses:             map[string]int{},
		repoCount:          newSpring1d(7.0, 1.0), // counts up, no overshoot
		editorID:           editor.DefaultID(),
		sortMode:           sortName, // launch sorted by name (not the sortAttention zero value)
		autoRefresh:        true,
		loading:            true,
		status:             tendingLine(),
		assistantCmd:       aCmd,
		assistantLabel:     aLabel,
		idleGen:            1,
		idleAfter:          idleSeconds(),
	}
}

// displayRoot resolves the scan root to an absolute path with the home dir
// abbreviated to ~, so the header never shows a confusing relative path like "..".
func displayRoot(root string) string {
	p := repo.ExpandPath(root)
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(p, home) {
		p = "~" + p[len(home):]
	}
	return p
}

func (m model) Init() tea.Cmd {
	return tea.Batch(scanCmd(m.root, m.concurrency), tickCmd(), fetchTickCmd(), idleTickCmd(idleProbe, m.idleGen), updateCheckCmd(m.version))
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resize()
		// First real size: the orchard grows in once before the dashboard appears.
		if !m.introShown && animEnabled() && m.width >= 40 && m.height >= 10 {
			m.introShown = true
			m.intro = newIntro(m.innerWidth(), max(1, m.height-2))
			return m, m.startAnim()
		}
		return m, nil

	case scanMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err.Error()
			m.status = "scan failed"
			return m, nil
		}
		m.repos = msg.repos
		m.dropMissingSelections()
		m.status = fmt.Sprintf("scanned %d repos", len(msg.repos))
		m.claudeUsage = nil // recompute the pinned usage panel
		m.syncRows()
		// Count the REPOS metric up; while the intro plays it defers to intro-end.
		var countCmd tea.Cmd
		if m.intro == nil {
			m.beginCountUp()
			m.beginReveal()
			countCmd = m.startAnim()
		}
		// recompute languages on every manual scan (startup / refresh / after
		// clone) so newly-added repos get their language; claude usage too.
		cmds := []tea.Cmd{claudeStatsCmd(m.repos), langCmd(m.repos), ghStatusCmd(m.repos), instrCmd(m.repos), countCmd}
		if !m.seenChecked { // "since last visit" baseline: once per launch only
			m.seenChecked = true
			cmds = append(cmds, newCommitsCmd(m.repos))
		}
		return m, tea.Batch(cmds...)

	case newCommitsMsg:
		m.newByPath = msg.byPath
		m.syncRows()
		return m, nil

	case langMsg:
		m.langByPath = msg.byPath
		m.syncRows()
		return m, nil

	case worklogMsg:
		if m.mode == modeWorklog {
			m.worklogText = msg.text
			m.detailVP.SetContent(m.worklogBody(m.detailVP.Width, msg))
			m.detailVP.GotoTop()
		}
		return m, nil

	case cloneDoneMsg:
		if msg.err != nil {
			m.loading = false
			m.err = ""
			m.status = "clone failed: " + firstLine(msg.err.Error())
			return m, nil
		}
		m.status = "cloned " + msg.name + " · refreshing"
		return m, scanCmd(m.root, m.concurrency) // pick up the new repo

	case pullOneMsg:
		m.applyOneResult(msg.result)
		delete(m.pulling, msg.result.Repo.Path)
		var pulseCmd tea.Cmd
		switch msg.result.Status {
		case orchardgit.StatusPulled:
			m.pullDone++
			pulseCmd = m.pulse(msg.result.Repo.Path) // the repo blossoms: just tended
		case orchardgit.StatusSkipped:
			m.pullSkip++
		case orchardgit.StatusFailed:
			m.pullFail++
		}
		if len(m.pulling) == 0 {
			m.loading = false
			m.status = fmt.Sprintf("pull complete: %d pulled · %d skipped · %d failed", m.pullDone, m.pullSkip, m.pullFail)
		}
		m.syncRows()
		return m, pulseCmd

	case fetchOneMsg:
		for i, r := range m.repos {
			if r.Path == msg.repo.Path {
				m.repos[i] = msg.repo
				break
			}
		}
		delete(m.pulling, msg.repo.Path)
		if len(m.pulling) == 0 {
			m.loading = false
			m.status = "fetch complete"
		}
		m.syncRows()
		return m, nil

	case statusMsg:
		m.status = msg.text
		return m, nil

	case claudeStatsMsg:
		u := msg.usage
		m.claudeUsage = &u
		m.resize() // panel may now appear/disappear; let the list reclaim rows
		return m, nil

	case tickMsg:
		// always re-arm; only refresh when idle and on the list (no flicker mid-action)
		if m.autoRefresh && !m.loading && m.mode == modeList && len(m.pulling) == 0 {
			return m, tea.Batch(tickCmd(), silentScanCmd(m.root, m.concurrency))
		}
		return m, tickCmd()

	case fetchTickMsg:
		// Background fetch only while live refresh is on and the dashboard is idle,
		// and never while one is already running, so ahead/behind go live without
		// hammering the network. Always re-arm the (slow) ticker.
		if m.autoRefresh && !m.loading && !m.bgFetching && m.mode == modeList && len(m.pulling) == 0 && !demoMode() && len(m.repos) > 0 {
			m.bgFetching = true
			return m, tea.Batch(fetchTickCmd(), bgFetchCmd(m.root, m.repos, m.concurrency))
		}
		return m, fetchTickCmd()

	case bgFetchMsg:
		m.bgFetching = false
		if len(msg.repos) > 0 {
			m.repos = msg.repos
			m.dropMissingSelections()
			m.syncRows()
		}
		return m, nil

	case bloomTickMsg:
		if m.bloomFrames > 0 {
			m.bloomFrames--
		}
		if m.bloomFrames > 0 {
			return m, bloomTickCmd()
		}
		return m, nil

	case animTickMsg:
		if m.stepAnims() {
			return m, animTick()
		}
		m.animOn = false
		return m, nil

	case commitTickMsg:
		if m.mode == modeCommitMsg && m.commitMsgLoading {
			m.commitMsgFrame++
			return m, commitTick()
		}
		return m, nil

	case idleTickMsg:
		if msg.gen != m.idleGen {
			return m, nil // superseded by a fresher tick
		}
		// Only the bare dashboard sleeps; any modal/loading keeps it awake.
		if m.mode != modeList || m.loading || m.filtering {
			m.idle, m.ssActive = 0, false
			return m, idleTickCmd(idleProbe, m.idleGen)
		}
		m.idle++
		if !m.ssActive && m.idleAfter > 0 && m.idle >= m.idleAfter {
			m.ssActive, m.ssFrame = true, 0
		}
		next := idleProbe
		if m.ssActive {
			m.ssFrame++
			next = ssFrameDur
		}
		return m, idleTickCmd(next, m.idleGen)

	case silentScanMsg:
		if len(msg.repos) == 0 {
			return m, nil
		}
		m.repos = msg.repos
		m.dropMissingSelections()
		m.syncRows()
		return m, nil

	case branchesMsg:
		if msg.path == m.branchRepo {
			m.branchLoading = false
			m.branchAll = msg.branches
			m.branchCursor = 0
			if msg.err != nil {
				m.status = "branches: " + msg.err.Error()
			}
		}
		return m, nil

	case sessionsMsg:
		if msg.path == m.sessionsRepo.Path {
			m.sessionsLoading = false
			m.sessions = msg.sessions
			m.sessionCursor = 0
		}
		return m, nil

	case ghStatusMsg:
		m.ghStatus = msg.byPath
		m.syncRows() // a failing-CI flag may now show in the info column
		return m, nil

	case updateMsg:
		if msg.available {
			m.updateTag = msg.tag
		}
		return m, nil

	case tea.MouseMsg:
		if m.ssActive {
			return m, nil
		}
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			if usesDetailVP(m.mode) {
				m.detailVP.ScrollUp(2)
				return m, nil
			}
			m.scrollActive(-1)
		case tea.MouseButtonWheelDown:
			if usesDetailVP(m.mode) {
				m.detailVP.ScrollDown(2)
				return m, nil
			}
			m.scrollActive(1)
		case tea.MouseButtonLeft:
			if m.mode == modeList && msg.Action == tea.MouseActionPress {
				m.clickToRow(msg.Y) // click selects/deselects the row (enter opens detail)
				m.syncRows()
			}
		}
		return m, nil

	case instrMsg:
		m.instructionsByPath = msg.byPath
		if m.mode == modeDetail {
			m.setDetailContent() // reflect a just-wired CLAUDE.md in the detail view
		}
		m.syncRows()
		return m, nil

	case wireInstrMsg:
		if msg.err != "" {
			m.status = "wiring failed: " + msg.err
		} else {
			m.status = fmt.Sprintf("wired %d CLAUDE.md → AGENTS.md (%d skipped)", msg.wired, msg.skipped)
		}
		return m, instrCmd(m.repos) // refresh health

	case statsMsg:
		m.statsLoading = false
		m.statsHarvest = msg.harvest
		m.statsClaude = msg.claude
		if m.mode == modeStats {
			m.detailVP.SetContent(m.statsBody(m.detailVP.Width))
		}
		return m, nil

	case sessionSearchMsg:
		if msg.query == m.sessionSearchQuery {
			m.sessionSearchRunning = false
			m.sessionSearchResults = msg.hits
			m.sessionSearchFocus = false
			m.sessionSearchInput.Blur()
			m.sessionSearchCursor = 0
		}
		return m, nil

	case commitMsgMsg:
		if msg.path == m.commitMsgRepo.Path {
			m.commitMsgLoading = false
			if msg.err != nil {
				m.commitMsgErr = "could not draft: " + msg.err.Error()
			} else if strings.TrimSpace(msg.text) == "" {
				m.commitMsgErr = "the assistant returned an empty message"
			} else {
				m.commitMsg = msg.text
			}
		}
		return m, nil

	case touchedMsg:
		if msg.path == m.touchedRepo.Path {
			m.touchedFiles = msg.files
			m.touchedDirty = msg.dirty
			m.touchedLoading = false
			m.touchedCursor = clamp(m.touchedCursor, 0, max(0, len(m.touchedFiles)-1))
		}
		return m, nil
	case diffMsg:
		if msg.path == m.diffRepo.Path {
			if msg.err != nil {
				m.diffText = ""
				m.detailVP.SetContent(fillLine(errorStyle.Render("  diff: "+msg.err.Error()), m.detailVP.Width, bg))
			} else {
				m.diffText = msg.text
				m.detailVP.SetContent(colorizeDiff(msg.text, m.detailVP.Width))
			}
			m.detailVP.GotoTop()
		}
		return m, nil

	case checkoutMsg:
		m.branchBusy = false
		if msg.err != nil {
			e := msg.err.Error()
			friendly := "checkout failed: " + firstLine(e)
			if strings.Contains(e, "would be overwritten") || strings.Contains(e, "local changes") {
				friendly = msg.repo.Name + " has uncommitted changes"
			}
			// keep the branch modal open and show the error there; fall back to the
			// status line if the modal was already closed.
			if m.mode == modeBranch {
				m.branchErr = friendly
			} else {
				m.status = friendly
			}
		} else {
			for i, r := range m.repos {
				if r.Path == msg.repo.Path {
					m.repos[i] = msg.repo
					break
				}
			}
			m.mode = modeList
			m.branchInput.Blur()
			m.branchErr = ""
			m.branchTarget = ""
			m.status = msg.repo.Name + "  →  " + msg.branch
			if msg.stashed {
				m.status += "  ·  changes stashed (git stash pop to restore)"
			}
		}
		m.syncRows()
		return m, nil

	case searchResultMsg:
		if msg.query == m.searchQuery {
			m.searchRunning = false
			m.loading = false
			m.searchResults = msg.results
			m.searchCursor = 0
			m.searchVP.SetYOffset(0)
			m.flattenSearch()
			m.searchFocus = false
			m.setSearchContent()
		}
		return m, nil

	case detailMsg:
		if msg.path == m.detailRepo {
			st := &detailState{repo: m.repoByPath(msg.path), langs: msg.langs, sessions: msg.sessions, commitsSince: msg.commitsSince, touched: msg.touched}
			if msg.err != nil {
				st.err = msg.err.Error()
			} else {
				st.info = msg.info
			}
			m.detail = st
			m.setDetailContent()
			if strings.HasPrefix(m.status, "loading ") {
				m.status = ""
			}
		}
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		switch {
		case len(m.pulling) > 0:
			m.syncRows()
			return m, cmd
		case m.mode == modeDetail && m.detail == nil:
			m.setDetailContent() // re-render the animated loading line in the viewport
			return m, cmd
		case m.mode == modeTouched && m.touchedLoading:
			return m, cmd // touchedView reads m.spinner.View() on each render
		}
		return m, nil // nothing loading: let the tick chain stop

	case tea.KeyMsg:
		// Any key skips the launch intro (ctrl+c still quits), then reveals the
		// dashboard with the count-up.
		if m.intro != nil {
			if msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			m.intro = nil
			m.beginCountUp()
			return m, m.startAnim()
		}
		// Any key wakes the screensaver (and is consumed doing so) and resets idle.
		if m.ssActive {
			m.ssActive, m.idle = false, 0
			m.idleGen++
			return m, idleTickCmd(idleProbe, m.idleGen)
		}
		m.idle = 0
		if m.revealActive { // any key completes the row cascade instantly
			m.revealActive = false
			m.syncRows()
		}
		if m.filtering {
			return m.handleFilterKey(msg)
		}
		// Scrolling the shared detail viewport is handled from one place (instant,
		// not eased), so every pager (detail, diff, stats, help, worklog, docs)
		// behaves identically and g/G work everywhere.
		if usesDetailVP(m.mode) {
			switch msg.String() {
			case "up", "k":
				m.detailVP.ScrollUp(1)
				return m, nil
			case "down", "j":
				m.detailVP.ScrollDown(1)
				return m, nil
			case "pgup":
				m.detailVP.ScrollUp(max(1, m.detailVP.Height))
				return m, nil
			case "pgdown":
				m.detailVP.ScrollDown(max(1, m.detailVP.Height))
				return m, nil
			case "g", "home":
				m.detailVP.GotoTop()
				return m, nil
			case "G", "end":
				m.detailVP.GotoBottom()
				return m, nil
			}
		}
		switch m.mode {
		case modeHelp:
			return m.handleHelpKey(msg)
		case modeClone:
			return m.handleCloneKey(msg)
		case modeWorklog:
			return m.handleWorklogKey(msg)
		case modeEditor:
			return m.handleEditorKey(msg)
		case modeBranch:
			return m.handleBranchKey(msg)
		case modeSessions:
			return m.handleSessionsKey(msg)
		case modeDiff:
			return m.handleDiffKey(msg)
		case modeStats:
			return m.handleStatsKey(msg)
		case modeCommitMsg:
			return m.handleCommitMsgKey(msg)
		case modeSessionSearch:
			return m.handleSessionSearchKey(msg)
		case modePresets:
			return m.handlePresetsKey(msg)
		case modeTouched:
			return m.handleTouchedKey(msg)
		case modePreview:
			return m.handlePreviewKey(msg)
		case modeSearch:
			return m.handleSearchKey(msg)
		case modeDetail:
			return m.handleDetailKey(msg)
		case modeConfirm:
			return m.handleConfirmKey(msg)
		default:
			return m.handleListKey(msg)
		}
	}
	return m, nil
}

func (m *model) resize() {
	inner := m.innerWidth()
	m.viewport.Width = inner
	// header(3)+metrics(1)+grid header(1)+footer(2)+padding(2)=9, plus the
	// Claude panel(3) when it is shown. When hidden, the list reclaims those rows.
	chrome := 9
	if m.showClaudePanel() {
		chrome += 3
	}
	m.viewport.Height = clamp(m.height-chrome, 3, max(3, m.height))
	m.detailVP.Width = inner
	m.detailVP.Height = clamp(m.height-7, 3, max(3, m.height))
	m.searchVP.Width = inner
	m.searchVP.Height = clamp(m.height-8, 3, max(3, m.height))
	m.filterInput.Width = clamp(inner-20, 10, 80)
	m.searchInput.Width = clamp(inner-20, 10, 100)
	m.ensureCursorVisible()
	m.syncRows()
	if m.detail != nil {
		m.setDetailContent()
	}
	if m.mode == modeSearch {
		// Re-render at the new size so width-based sizing and the keep-selected
		// -visible scroll math run against the new searchVP dimensions.
		m.setSearchContent()
	}
	if m.mode == modeDiff {
		m.detailVP.SetContent(colorizeDiff(m.diffText, m.detailVP.Width))
	}
	if m.mode == modePreview {
		m.setPreviewContent()
	}
	if m.mode == modeStats {
		m.detailVP.SetContent(m.statsBody(m.detailVP.Width))
	}
}

func (m model) innerWidth() int {
	if m.width <= 0 {
		return 96
	}
	return max(56, m.width-4)
}

func (m model) View() string {
	inner := m.innerWidth()
	if m.intro != nil {
		return appStyle.Width(inner + 4).Height(max(1, m.height)).Render(m.intro.view(inner, max(1, m.height-2)))
	}
	switch m.mode {
	case modeDetail:
		return appStyle.Width(inner + 4).Height(max(1, m.height)).Render(m.detailView(inner))
	case modeEditor:
		return appStyle.Width(inner + 4).Height(max(1, m.height)).Render(m.overlayModal(m.editorView(inner), inner))
	case modeBranch:
		return appStyle.Width(inner + 4).Height(max(1, m.height)).Render(m.overlayModal(m.branchView(inner), inner))
	case modeSessions:
		return appStyle.Width(inner + 4).Height(max(1, m.height)).Render(m.overlayModal(m.sessionsView(inner), inner))
	case modeCommitMsg:
		return appStyle.Width(inner + 4).Height(max(1, m.height)).Render(m.overlayModal(m.commitMsgView(inner), inner))
	case modeSessionSearch:
		return appStyle.Width(inner + 4).Height(max(1, m.height)).Render(m.sessionSearchView(inner))
	case modePresets:
		return appStyle.Width(inner + 4).Height(max(1, m.height)).Render(m.overlayModal(m.presetsView(inner), inner))
	case modeTouched:
		return appStyle.Width(inner + 4).Height(max(1, m.height)).Render(m.overlayModal(m.touchedView(inner), inner))
	case modePreview:
		return appStyle.Width(inner + 4).Height(max(1, m.height)).Render(m.previewView(inner))
	case modeDiff:
		return appStyle.Width(inner + 4).Height(max(1, m.height)).Render(m.diffView(inner))
	case modeStats:
		return appStyle.Width(inner + 4).Height(max(1, m.height)).Render(m.statsView(inner))
	case modeHelp:
		return appStyle.Width(inner + 4).Height(max(1, m.height)).Render(m.helpView(inner))
	case modeWorklog:
		return appStyle.Width(inner + 4).Height(max(1, m.height)).Render(m.worklogView(inner))
	case modeClone:
		return appStyle.Width(inner + 4).Height(max(1, m.height)).Render(m.overlayModal(m.cloneView(inner), inner))
	case modeConfirm:
		return appStyle.Width(inner + 4).Height(max(1, m.height)).Render(m.overlayModal(m.confirmView(inner), inner))
	case modeSearch:
		return appStyle.Width(inner + 4).Height(max(1, m.height)).Render(m.searchView(inner))
	default:
		if m.ssActive {
			return appStyle.Width(inner + 4).Height(max(1, m.height)).Render(screensaverView(inner, max(1, m.height-2), m.ssFrame))
		}
		return appStyle.Width(inner + 4).Height(max(1, m.height)).Render(m.dashboardBody(inner))
	}
}

// dashboardBody renders the main list screen (header, metrics, grid, Claude
// panel, footer). It doubles as the backdrop behind floating modals.
func (m model) dashboardBody(inner int) string {
	rows := []string{
		m.headerView(inner),
		m.metricsView(inner),
		m.gridView(inner),
	}
	if m.showClaudePanel() {
		rows = append(rows, m.claudePanel(inner))
	}
	rows = append(rows, m.footerView(inner))
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}
