package tui

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	orchardgit "github.com/prakashkurup/orchard/internal/git"
	"github.com/prakashkurup/orchard/internal/github"
	"github.com/prakashkurup/orchard/internal/repo"
)

type ghStatusMsg struct {
	byPath map[string]github.RepoStatus
}

// githubTarget resolves a repo to its github.com owner/name and the ref to check
// CI on (its default branch). ok is false for non-GitHub or remoteless repos.
func githubTarget(ctx context.Context, r repo.Repo) (owner, name, ref string, ok bool) {
	web := orchardgit.WebURL(ctx, r.Path)
	if web == "" {
		return "", "", "", false
	}
	u, err := url.Parse(web)
	if err != nil || u.Host != "github.com" {
		return "", "", "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", "", false
	}
	ref = r.DefaultBranch
	if ref == "" {
		ref = "main"
	}
	return parts[0], parts[1], ref, true
}

// ghStatusCmd fetches open PRs + CI status for every GitHub repo, concurrently.
// It is skipped entirely when no token is configured (no per-repo failures), and
// runs only on the initial scan and manual refresh, never the silent auto-refresh.
func ghStatusCmd(repos []repo.Repo) tea.Cmd {
	if demoMode() {
		return func() tea.Msg { return ghStatusMsg{byPath: demoGHStatus()} }
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if !github.HasToken(ctx) {
			return ghStatusMsg{} // no token: GitHub status unavailable, skip quietly
		}
		out := map[string]github.RepoStatus{}
		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, 8)
		for _, r := range repos {
			wg.Add(1)
			go func(r repo.Repo) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				owner, name, ref, ok := githubTarget(ctx, r)
				if !ok {
					return
				}
				if st := github.RepoStatusFor(ctx, owner, name, ref); st.Err == nil {
					mu.Lock()
					out[r.Path] = st
					mu.Unlock()
				}
			}(r)
		}
		wg.Wait()
		return ghStatusMsg{byPath: out}
	}
}
