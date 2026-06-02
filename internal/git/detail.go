package git

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/prakashkurup/orchard/internal/repo"
)

// Commit is a single line in a repo's recent history.
type Commit struct {
	Hash    string `json:"hash"`
	Rel     string `json:"rel"`
	Subject string `json:"subject"`
	Author  string `json:"author"`
}

// GraphRow is one line of `git log --graph` output: the rail characters plus,
// when the line carries a commit, its metadata.
type GraphRow struct {
	Rail     string `json:"rail"`
	IsCommit bool   `json:"is_commit"`
	Hash     string `json:"hash,omitempty"`
	Rel      string `json:"rel,omitempty"`
	Subject  string `json:"subject,omitempty"`
	Author   string `json:"author,omitempty"`
}

// DetailInfo is the expanded view of one repository.
type DetailInfo struct {
	Branch      string     `json:"branch"`
	Upstream    string     `json:"upstream"`
	StatusLines []string   `json:"status_lines"`
	Commits     []Commit   `json:"commits"`
	Graph       []GraphRow `json:"graph"`
	Remotes     []string   `json:"remotes"`
}

// Detail gathers the working-tree status, recent commits and remotes for one
// repository, for the detail view.
func Detail(ctx context.Context, r repo.Repo) (DetailInfo, error) {
	info := DetailInfo{Branch: r.Branch, Upstream: r.Upstream}

	status, err := runGitRaw(ctx, r.Path, "status", "--porcelain")
	if err != nil {
		return info, err // a broken or removed repo surfaces as an error, not a blank page
	}
	for _, line := range strings.Split(status, "\n") {
		if strings.TrimSpace(line) != "" {
			info.StatusLines = append(info.StatusLines, line)
		}
	}

	// %x1f is the unit separator; safe inside commit subjects/authors.
	if log, err := runGit(ctx, r.Path, "log", "-8", "--format=%h%x1f%cr%x1f%s%x1f%an"); err == nil {
		for _, line := range strings.Split(log, "\n") {
			parts := strings.Split(line, "\x1f")
			if len(parts) != 4 {
				continue
			}
			info.Commits = append(info.Commits, Commit{
				Hash: parts[0], Rel: parts[1], Subject: parts[2], Author: parts[3],
			})
		}
	}

	// commit graph with real branch/merge topology (vscode-style); capped, with
	// the rest available in your editor / git log / the browser.
	if g, err := runGit(ctx, r.Path, "log", "--graph", "-n", "30", "--abbrev=7",
		"--format=%x1e%h%x1f%cr%x1f%s%x1f%an"); err == nil {
		for _, line := range strings.Split(g, "\n") {
			idx := strings.IndexByte(line, '\x1e')
			if idx < 0 {
				info.Graph = append(info.Graph, GraphRow{Rail: strings.TrimRight(line, " ")})
				continue
			}
			row := GraphRow{Rail: strings.TrimRight(line[:idx], " "), IsCommit: true}
			f := strings.Split(line[idx+1:], "\x1f")
			if len(f) == 4 {
				row.Hash, row.Rel, row.Subject, row.Author = f[0], f[1], f[2], f[3]
			}
			info.Graph = append(info.Graph, row)
		}
	}

	if remotes, err := runGit(ctx, r.Path, "remote", "-v"); err == nil {
		seen := map[string]bool{}
		for _, line := range strings.Split(remotes, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			// "origin  git@... (fetch)" -> "origin  git@..."
			if idx := strings.LastIndex(line, " ("); idx != -1 {
				line = line[:idx]
			}
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				// strip any embedded token (https://user:tok@host/…) before display
				fields[len(fields)-1] = stripCredentials(fields[len(fields)-1])
			}
			line = strings.Join(fields, "  ")
			if !seen[line] {
				seen[line] = true
				info.Remotes = append(info.Remotes, line)
			}
		}
	}

	return info, nil
}

// AuthorEmail returns the effective git user.email for a repo (local or global).
func AuthorEmail(ctx context.Context, path string) string {
	out, err := runGit(ctx, path, "config", "user.email")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// Worklog returns your own commits in a repo within the time window (e.g.
// "1 day ago"), newest first - for the cross-repo activity digest.
func Worklog(ctx context.Context, path, since string) []Commit {
	author := AuthorEmail(ctx, path)
	if author == "" {
		return nil
	}
	out, err := runGit(ctx, path, "log", "--author="+author, "--since="+since,
		"--format=%h%x1f%cr%x1f%s%x1f%an", "-n", "50")
	if err != nil || strings.TrimSpace(out) == "" {
		return nil
	}
	var cs []Commit
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, "\x1f")
		if len(f) != 4 {
			continue
		}
		cs = append(cs, Commit{Hash: f[0], Rel: f[1], Subject: f[2], Author: f[3]})
	}
	return cs
}

// AuthoredDays returns the committer dates (YYYY-MM-DD) of your own commits in a
// repo since the given window, one entry per commit, for the harvest heatmap.
func AuthoredDays(ctx context.Context, path, since string) []string {
	author := AuthorEmail(ctx, path)
	if author == "" {
		return nil
	}
	out, err := runGitRaw(ctx, path, "log", "--author="+author, "--since="+since,
		"--date=format:%Y-%m-%d", "--format=%cd")
	if err != nil {
		return nil
	}
	var days []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			days = append(days, line)
		}
	}
	return days
}

// Diff returns the working-tree diff against HEAD (staged + unstaged) for the
// inline diff viewer; an empty string means a clean tree. Falls back to a plain
// diff when the repo has no commits yet (no HEAD).
func Diff(ctx context.Context, path string, pathspec ...string) (string, error) {
	build := func(base ...string) []string {
		if len(pathspec) == 0 {
			return base
		}
		return append(append(base, "--"), pathspec...)
	}
	out, err := runGitRaw(ctx, path, build("diff", "HEAD")...)
	if err != nil {
		out, err = runGitRaw(ctx, path, build("diff")...)
	}
	// An untracked file (a new file the agent created) has an empty HEAD diff and
	// no error; show its full content so a single-file diff is never "clean".
	if err == nil && strings.TrimSpace(out) == "" && len(pathspec) == 1 {
		if u := untrackedDiff(ctx, path, pathspec[0]); u != "" {
			return u, nil
		}
	}
	return out, err
}

// untrackedDiff returns the full content of an untracked file as an add-only diff
// via `git diff --no-index` (which exits non-zero when files differ, so its
// stdout is read directly). Empty for tracked or absent paths.
func untrackedDiff(ctx context.Context, dir, rel string) string {
	st, err := runGitRaw(ctx, dir, "status", "--porcelain", "--", rel)
	if err != nil || !strings.HasPrefix(strings.TrimSpace(st), "??") {
		return ""
	}
	out, _ := exec.CommandContext(ctx, "git", "-C", dir, "diff", "--no-index", "--", os.DevNull, rel).Output()
	return strings.TrimRight(string(out), "\r\n")
}

// StatusLines returns the non-empty `git status --porcelain` lines, a lightweight
// way to learn which files are dirty without the full Detail gather.
func StatusLines(ctx context.Context, path string) ([]string, error) {
	out, err := runGitRaw(ctx, path, "status", "--porcelain")
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines, nil
}

// WebURL returns the browsable https URL for a repo's origin remote, or "".
func WebURL(ctx context.Context, path string) string {
	raw, err := runGit(ctx, path, "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	return toWebURL(raw)
}

// toWebURL converts a git remote URL (scp-style git@…, ssh://…, or plain https)
// into a browsable https URL, stripping any trailing ".git" and any embedded
// credentials. It is the pure core of WebURL, separated so it can be table-tested
// without a real repository.
func toWebURL(raw string) string {
	u := strings.TrimSuffix(strings.TrimSpace(raw), ".git")
	switch {
	case strings.HasPrefix(u, "git@"):
		u = "https://" + strings.Replace(strings.TrimPrefix(u, "git@"), ":", "/", 1)
	case strings.HasPrefix(u, "ssh://"):
		u = "https://" + strings.TrimPrefix(strings.TrimPrefix(u, "ssh://"), "git@")
	}
	return stripCredentials(u)
}

// stripCredentials removes any userinfo embedded in a URL (e.g. a token in
// https://x-access-token:SECRET@host/…), so credentials never reach the browser
// opener's argv or the status line.
func stripCredentials(u string) string {
	parsed, err := url.Parse(u)
	if err != nil || parsed.User == nil {
		return u
	}
	parsed.User = nil
	return parsed.String()
}
