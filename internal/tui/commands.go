package tui

import (
	"context"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/prakashkurup/orchard/internal/claude"
	orchardgit "github.com/prakashkurup/orchard/internal/git"
	"github.com/prakashkurup/orchard/internal/lang"
	"github.com/prakashkurup/orchard/internal/repo"
	"github.com/prakashkurup/orchard/internal/seen"
	"net/url"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// refreshInterval is how often the dashboard silently re-scans local git state.
const refreshInterval = 30 * time.Second

func tickCmd() tea.Cmd {
	return tea.Tick(refreshInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// fetchTickCmd drives the background fetch cadence (slower than the local
// refresh). Returns nil when background fetching is disabled, so the ticker
// simply stops.
func fetchTickCmd() tea.Cmd {
	secs := fetchIntervalSecs()
	if secs <= 0 {
		return nil
	}
	return tea.Tick(time.Duration(secs)*time.Second, func(t time.Time) tea.Msg { return fetchTickMsg(t) })
}

// bgFetchCmd fetches every repo's remotes quietly in the background, then
// re-scans so ahead/behind reflect the live remote. Errors are swallowed.
func bgFetchCmd(root string, repos []repo.Repo, concurrency int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		orchardgit.FetchAllQuiet(ctx, repos, concurrency)
		updated, err := orchardgit.Scan(ctx, root, concurrency)
		if err != nil {
			return bgFetchMsg{}
		}
		return bgFetchMsg{repos: updated}
	}
}

// idleTickCmd drives idle detection and the screensaver animation. The gen token
// lets a fresh tick supersede any in-flight one (e.g. when cadence changes) so we
// never run two overlapping tickers.
func idleTickCmd(d time.Duration, gen int) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return idleTickMsg{gen: gen} })
}

// langCmd detects each repo's dominant language (concurrent, once per launch).
func langCmd(repos []repo.Repo) tea.Cmd {
	if demoMode() {
		return func() tea.Msg { return langMsg{byPath: demoLangs()} }
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		byPath := map[string]lang.Stat{}
		sem := make(chan struct{}, 8)
		var mu sync.Mutex
		var wg sync.WaitGroup
		for _, r := range repos {
			wg.Add(1)
			go func(r repo.Repo) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				if s := lang.Dominant(ctx, r.Path); s.Name != "" {
					mu.Lock()
					byPath[r.Path] = s
					mu.Unlock()
				}
			}(r)
		}
		wg.Wait()
		return langMsg{byPath: byPath}
	}
}

// newCommitsCmd compares each repo's HEAD to the sha saved at the last visit,
// counts new commits, then persists the current HEADs for next time.
func newCommitsCmd(repos []repo.Repo) tea.Cmd {
	if demoMode() {
		return func() tea.Msg { return newCommitsMsg{byPath: demoNew()} }
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		prev := seen.Load()
		byPath := map[string]int{}
		now := map[string]string{}
		for _, r := range repos {
			now[r.Path] = r.Head
			if old := prev[r.Path]; old != "" && old != r.Head {
				if n := orchardgit.NewCommitCount(ctx, r.Path, old); n > 0 {
					byPath[r.Path] = n
				}
			}
		}
		// Only advance the baseline if the scan completed; a partial timeout
		// would otherwise bump HEADs past commits we never got to count.
		if ctx.Err() == nil {
			_ = seen.Save(now)
		}
		return newCommitsMsg{byPath: byPath}
	}
}

// silentScanCmd re-scans local git state without the loading flicker or the
// expensive claude.Aggregate rollup (it still does the cheap per-repo Claude
// filesystem summary), for background auto-refresh.
func silentScanCmd(root string, concurrency int) tea.Cmd {
	if demoMode() {
		return func() tea.Msg { return silentScanMsg{repos: demoRepos()} }
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		repos, err := orchardgit.Scan(ctx, root, concurrency)
		if err != nil {
			return silentScanMsg{}
		}
		enrichClaude(repos) // cheap filesystem summary; keeps sort-by-claude accurate
		return silentScanMsg{repos: repos}
	}
}

func (m *model) startPull(targets []repo.Repo) tea.Cmd {
	if len(targets) == 0 {
		m.status = "nothing to pull"
		return nil
	}
	m.pulling = map[string]bool{}
	for _, r := range targets {
		m.pulling[r.Path] = true
	}
	m.pullDone, m.pullSkip, m.pullFail = 0, 0, 0
	m.loading = true
	m.status = fmt.Sprintf("harvesting %d repos", len(targets))

	sem := make(chan struct{}, max(1, m.concurrency))
	cmds := make([]tea.Cmd, 0, len(targets)+1)
	cmds = append(cmds, m.spinner.Tick)
	for _, r := range targets {
		r := r
		cmds = append(cmds, func() tea.Msg {
			sem <- struct{}{}
			defer func() { <-sem }()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			return pullOneMsg{result: orchardgit.Pull(ctx, r)}
		})
	}
	return tea.Batch(cmds...)
}

func (m *model) startFetch(targets []repo.Repo) tea.Cmd {
	if len(targets) == 0 {
		m.status = "nothing to fetch"
		return nil
	}
	m.pulling = map[string]bool{}
	for _, r := range targets {
		m.pulling[r.Path] = true
	}
	m.loading = true
	m.status = fmt.Sprintf("checking %d trees", len(targets))

	sem := make(chan struct{}, max(1, m.concurrency))
	cmds := make([]tea.Cmd, 0, len(targets)+1)
	cmds = append(cmds, m.spinner.Tick)
	for _, r := range targets {
		r := r
		cmds = append(cmds, func() tea.Msg {
			sem <- struct{}{}
			defer func() { <-sem }()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			updated, err := orchardgit.Fetch(ctx, r)
			return fetchOneMsg{repo: updated, err: err}
		})
	}
	return tea.Batch(cmds...)
}

// openCmd opens the repo's origin in the default browser.
func openCmd(r repo.Repo) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		target := orchardgit.WebURL(ctx, r.Path)
		if target == "" {
			return statusMsg{text: "no origin remote for " + r.Name}
		}
		// Only ever hand a plain http(s) URL to the OS opener - this blocks both
		// argument injection (a "-"-prefixed remote read as a flag) and arbitrary
		// URL schemes (file://, custom handlers) from a hostile origin remote.
		if !isWebURL(target) {
			return statusMsg{text: "refusing to open non-web URL: " + target}
		}
		cmd := exec.Command(browserOpener(), target)
		if err := cmd.Start(); err != nil {
			return statusMsg{text: "open failed: " + err.Error()}
		}
		go func() { _ = cmd.Wait() }() // reap the child so it doesn't linger as a zombie
		return statusMsg{text: "opened " + target}
	}
}

// openMatchURLCmd opens a search match on the repo's web host (e.g. GitHub) at
// the exact line: <web>/blob/<branch>/<path>#L<line>.
func openMatchURLCmd(repoPath, file string, line int, branch string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		web := orchardgit.WebURL(ctx, repoPath)
		if web == "" {
			return statusMsg{text: "no web origin for this repo"}
		}
		target := fmt.Sprintf("%s/blob/%s/%s#L%d", web, url.PathEscape(branch), escapePath(file), line)
		if !isWebURL(target) {
			return statusMsg{text: "refusing to open non-web URL: " + target}
		}
		cmd := exec.Command(browserOpener(), target)
		if err := cmd.Start(); err != nil {
			return statusMsg{text: "open failed: " + err.Error()}
		}
		go func() { _ = cmd.Wait() }()
		return statusMsg{text: "opened " + target}
	}
}

// isWebURL reports whether u is a plain http/https URL with a host and no
// embedded credentials - the only shape we pass to the browser opener.
func isWebURL(u string) bool {
	parsed, err := url.Parse(u)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

// escapePath percent-encodes each segment of a slash-separated repo path so a
// file name can never inject extra path/query/fragment parts into a blob URL.
func escapePath(p string) string {
	parts := strings.Split(filepath.ToSlash(p), "/")
	for i, seg := range parts {
		parts[i] = url.PathEscape(seg)
	}
	return strings.Join(parts, "/")
}

func browserOpener() string {
	switch runtime.GOOS {
	case "darwin":
		return "open"
	case "windows":
		return "explorer"
	default:
		return "xdg-open"
	}
}

func (m *model) applyOneResult(res orchardgit.PullResult) {
	for i, r := range m.repos {
		if r.Path != res.Repo.Path {
			continue
		}
		updated := res.Repo
		switch res.Status {
		case orchardgit.StatusPulled:
			updated.SkipReason = ""
		case orchardgit.StatusSkipped:
			updated.SkipReason = res.Reason
		case orchardgit.StatusFailed:
			updated.SkipReason = res.Error
		}
		m.repos[i] = updated
		return
	}
}

func scanCmd(root string, concurrency int) tea.Cmd {
	if demoMode() {
		return func() tea.Msg { return scanMsg{repos: demoRepos()} }
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		repos, err := orchardgit.Scan(ctx, root, concurrency)
		if err == nil {
			enrichClaude(repos)
		}
		return scanMsg{repos: repos, err: err}
	}
}

// enrichClaude annotates each repo with its Claude Code session summary (cheap,
// filesystem-only).
func enrichClaude(repos []repo.Repo) {
	for i := range repos {
		n, last := claude.Summary(repos[i].Path)
		repos[i].CCSessions = n
		repos[i].CCLast = last
	}
}
