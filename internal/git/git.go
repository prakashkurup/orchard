// Package git wraps the git CLI to scan, inspect, fetch, and pull local
// repositories concurrently.
package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prakashkurup/orchard/internal/repo"
)

const DefaultConcurrency = 8

// Pull result statuses, the allowed values of PullResult.Status.
const (
	StatusPulled  = "pulled"
	StatusSkipped = "skipped"
	StatusFailed  = "failed"
)

// PullResult is the outcome of pulling one repository.
type PullResult struct {
	Repo   repo.Repo `json:"repo"`
	Status string    `json:"status"`
	Reason string    `json:"reason,omitempty"`
	Error  string    `json:"error,omitempty"`
}

// Scan discovers repositories under root and returns each with its git status.
func Scan(ctx context.Context, root string, concurrency int) ([]repo.Repo, error) {
	seeds, err := repo.Discover(root)
	if err != nil {
		return nil, err
	}
	return StatusAll(ctx, seeds, concurrency), nil
}

// StatusAll resolves the git status of every seed concurrently, sorted by name.
func StatusAll(ctx context.Context, seeds []repo.Repo, concurrency int) []repo.Repo {
	if concurrency <= 0 {
		concurrency = DefaultConcurrency
	}

	type result struct {
		index int
		repo  repo.Repo
	}

	results := make(chan result, len(seeds))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i, seed := range seeds {
		wg.Add(1)
		go func(i int, seed repo.Repo) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				seed.Err = ctx.Err().Error()
				seed.Display = repo.ComputeDisplay(seed)
				results <- result{index: i, repo: seed}
				return
			}
			r, err := Status(ctx, seed)
			if err != nil && r.Err == "" {
				r.Err = err.Error()
				r.Display = repo.ComputeDisplay(r)
			}
			results <- result{index: i, repo: r}
		}(i, seed)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	out := make([]repo.Repo, len(seeds))
	for res := range results {
		out[res.index] = res.repo
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// Status fills in one repo's branch, dirty/stash/ahead-behind counts, upstream,
// last commit and fetch time, returning it with its display state computed.
func Status(ctx context.Context, seed repo.Repo) (repo.Repo, error) {
	r := seed
	r.Err = ""
	r.SkipReason = ""

	branch, err := runGit(ctx, r.Path, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		sha, shaErr := runGit(ctx, r.Path, "rev-parse", "--short", "HEAD")
		if shaErr != nil {
			r.Err = err.Error()
			return r.WithDisplay(), err
		}
		r.Detached = true
		r.Branch = "detached:" + sha
	} else {
		r.Branch = branch
		r.Detached = false
	}

	r.DefaultBranch = defaultBranch(ctx, r.Path, r.Branch)
	r.OnDefault = !r.Detached && r.Branch != "" && r.Branch == r.DefaultBranch

	status, err := runGit(ctx, r.Path, "status", "--porcelain")
	if err != nil {
		r.Err = err.Error()
		return r.WithDisplay(), err
	}
	r.ChangedFiles = countLines(status)
	r.Dirty = r.ChangedFiles > 0

	if stashes, err := runGit(ctx, r.Path, "stash", "list"); err == nil {
		r.Stashes = countLines(stashes)
	}

	upstream, err := runGit(ctx, r.Path, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if err == nil {
		r.Upstream = upstream
		r.HasUpstream = true
		if counts, err := runGit(ctx, r.Path, "rev-list", "--left-right", "--count", "@{u}...HEAD"); err == nil {
			behind, ahead := parseAheadBehind(counts)
			r.Behind = behind
			r.Ahead = ahead
		}
	} else {
		r.Upstream = ""
		r.HasUpstream = false
		r.Ahead = 0
		r.Behind = 0
	}

	lastCommit, err := runGit(ctx, r.Path, "log", "-1", "--format=%cr%x09%s")
	if err == nil {
		r.LastCommit = lastCommit
	} else if r.LastCommit == "" {
		r.LastCommit = "no commits"
	}

	r.LastFetched = lastSyncedAt(r.Path)
	if head, err := runGit(ctx, r.Path, "rev-parse", "HEAD"); err == nil {
		r.Head = head
	}
	r.Activity = CommitActivity(ctx, r.Path)

	return r.WithDisplay(), nil
}

// activityWeeks is how many weekly buckets the dashboard sparkline summarizes.
const activityWeeks = 12

// CountCommitsSince returns how many commits on HEAD have a commit date after
// `since` (0 on error). Used to flag that a repo has moved on since Claude last
// ran there.
func CountCommitsSince(ctx context.Context, path string, since time.Time) int {
	out, err := runGit(ctx, path, "rev-list", "--count", "--since="+since.Format(time.RFC3339), "HEAD")
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(out))
	return n
}

// CommitActivity returns commit counts per week for the last activityWeeks weeks
// on the current branch, oldest bucket first, for the dashboard sparkline.
func CommitActivity(ctx context.Context, path string) []int {
	buckets := make([]int, activityWeeks)
	since := fmt.Sprintf("%d weeks ago", activityWeeks)
	out, err := runGitRaw(ctx, path, "log", "--no-merges", "--since="+since, "--format=%ct")
	if err != nil || strings.TrimSpace(out) == "" {
		return buckets
	}
	now := time.Now()
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		sec, err := strconv.ParseInt(line, 10, 64)
		if err != nil {
			continue
		}
		weeksAgo := int(now.Sub(time.Unix(sec, 0)).Hours()) / (24 * 7)
		if weeksAgo < 0 || weeksAgo >= activityWeeks {
			continue
		}
		buckets[activityWeeks-1-weeksAgo]++
	}
	return buckets
}

// NewCommitCount counts commits in HEAD that aren't reachable from `since` (the
// previously-seen sha) - i.e. how many new commits landed since last visit.
func NewCommitCount(ctx context.Context, path, since string) int {
	if since == "" {
		return 0
	}
	out, err := runGit(ctx, path, "rev-list", "--count", since+"..HEAD")
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func countLines(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

// lastSyncedAt reports the last time the repo was fetched or pulled, taken from
// the mtime of .git/FETCH_HEAD (written on every fetch/pull). Falls back to
// HEAD's mtime, then zero time when neither exists.
func lastSyncedAt(path string) time.Time {
	gitDir := filepath.Join(path, ".git")
	// Support worktrees / submodules where .git is a file pointing elsewhere.
	if info, err := os.Stat(gitDir); err == nil && !info.IsDir() {
		if data, err := os.ReadFile(gitDir); err == nil {
			if dir, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir: "); ok {
				gitDir = dir
			}
		}
	}
	for _, name := range []string{"FETCH_HEAD", "HEAD"} {
		if info, err := os.Stat(filepath.Join(gitDir, name)); err == nil {
			return info.ModTime()
		}
	}
	return time.Time{}
}

// Fetch runs `git fetch` for r and returns its refreshed status.
func Fetch(ctx context.Context, r repo.Repo) (repo.Repo, error) {
	if _, err := runGit(ctx, r.Path, "fetch", "--quiet"); err != nil {
		r.Err = err.Error()
		r.Display = repo.ComputeDisplay(r)
		return r, err
	}
	r.LastFetched = time.Now()
	return Status(ctx, r)
}

// Pull fast-forwards seed (skipping dirty/no-upstream repos) and reports the
// outcome as a PullResult.
func Pull(ctx context.Context, seed repo.Repo) PullResult {
	current, err := Status(ctx, seed)
	if err != nil && current.Err != "" {
		return PullResult{Repo: current, Status: StatusSkipped, Reason: current.PullSkipReason()}
	}

	if reason := current.PullSkipReason(); reason != "" {
		current.SkipReason = reason
		return PullResult{Repo: current, Status: StatusSkipped, Reason: reason}
	}

	if _, err := runGit(ctx, current.Path, "pull", "--ff-only", "--quiet"); err != nil {
		current.Err = err.Error()
		current.Display = repo.ComputeDisplay(current)
		return PullResult{Repo: current, Status: StatusFailed, Error: err.Error()}
	}

	updated, err := Status(ctx, current)
	if err != nil {
		updated.Err = err.Error()
		updated.Display = repo.ComputeDisplay(updated)
		return PullResult{Repo: updated, Status: StatusFailed, Error: err.Error()}
	}
	updated.JustPulled = true
	updated.LastFetched = time.Now()
	return PullResult{Repo: updated, Status: StatusPulled}
}

// PullRepos pulls every repo concurrently and returns the results in order.
func PullRepos(ctx context.Context, repos []repo.Repo, concurrency int) []PullResult {
	if concurrency <= 0 {
		concurrency = DefaultConcurrency
	}

	type result struct {
		index int
		item  PullResult
	}

	results := make(chan result, len(repos))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i, r := range repos {
		wg.Add(1)
		go func(i int, r repo.Repo) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				r.Err = ctx.Err().Error()
				results <- result{index: i, item: PullResult{Repo: r.WithDisplay(), Status: StatusFailed, Error: ctx.Err().Error()}}
				return
			}
			results <- result{index: i, item: Pull(ctx, r)}
		}(i, r)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	out := make([]PullResult, len(repos))
	for res := range results {
		out[res.index] = res.item
	}
	return out
}

// Branch is one ref offered by the branch switcher.
type Branch struct {
	Name    string `json:"name"`
	Current bool   `json:"current"`
	Remote  bool   `json:"remote"` // remote-only (no local branch yet)
	Rel     string `json:"rel"`    // relative commit date
}

// Branches lists local branches (newest commit first) plus remote-only branches
// from origin, for the branch switcher.
func Branches(ctx context.Context, r repo.Repo) ([]Branch, error) {
	// NOTE: for-each-ref does NOT interpret %xXX escapes (unlike git log), so the
	// separator must be a real 0x1f byte embedded in the format string.
	const us = "\x1f"
	out, err := runGit(ctx, r.Path, "for-each-ref", "--sort=-committerdate",
		"--format=%(refname:short)"+us+"%(HEAD)"+us+"%(committerdate:relative)", "refs/heads")
	if err != nil {
		return nil, err
	}
	var branches []Branch
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, us)
		if len(f) < 3 {
			continue
		}
		if leadingDash(f[0]) {
			continue // a "-x" ref would be read by `git checkout` as an option
		}
		branches = append(branches, Branch{Name: f[0], Current: f[1] == "*", Rel: f[2]})
		seen[f[0]] = true
	}

	if rout, err := runGit(ctx, r.Path, "for-each-ref", "--sort=-committerdate",
		"--format=%(refname:short)"+us+"%(committerdate:relative)", "refs/remotes/origin"); err == nil {
		for _, line := range strings.Split(rout, "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			f := strings.Split(line, us)
			if len(f) < 2 || !strings.HasPrefix(f[0], "origin/") {
				continue // skips origin/HEAD (short name "origin") and non-origin refs
			}
			name := strings.TrimPrefix(f[0], "origin/")
			if name == "HEAD" || seen[name] || leadingDash(name) {
				continue
			}
			branches = append(branches, Branch{Name: name, Remote: true, Rel: f[1]})
			seen[name] = true
		}
	}
	return branches, nil
}

// Checkout switches the repo to the given branch (git's DWIM creates a tracking
// branch for a remote-only name). Returns git's error output on failure.
//
// A branch name is a positional argument to `git checkout`, so a value beginning
// with "-" (which git's plumbing allows in a ref, e.g. a hostile upstream's
// refs/heads/-f) would be parsed as an option. We cannot use a "--" separator
// here (that forces pathspec interpretation and breaks branch switching), so we
// reject such names outright; Branches() already filters them from the picker.
func Checkout(ctx context.Context, path, branch string) error {
	if leadingDash(branch) {
		return fmt.Errorf("refusing to check out a branch name beginning with '-': %q", branch)
	}
	_, err := runGit(ctx, path, "checkout", branch)
	return err
}

// leadingDash reports whether s would be misread as a command-line option.
func leadingDash(s string) bool { return strings.HasPrefix(s, "-") }

// Stash saves the working tree and index (including untracked files) so a
// blocked branch switch can proceed; restore later with `git stash pop`.
func Stash(ctx context.Context, path string) error {
	_, err := runGit(ctx, path, "stash", "push", "-u", "-m", "orchard auto-stash")
	return err
}

// FilterByName returns the repos whose name matches the regexp pattern (all
// repos when pattern is empty).
func FilterByName(repos []repo.Repo, pattern string) ([]repo.Repo, error) {
	if pattern == "" {
		return repos, nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	filtered := make([]repo.Repo, 0, len(repos))
	for _, r := range repos {
		if re.MatchString(r.Name) {
			filtered = append(filtered, r)
		}
	}
	return filtered, nil
}

func parseAheadBehind(raw string) (behind int, ahead int) {
	fields := strings.Fields(raw)
	if len(fields) != 2 {
		return 0, 0
	}
	behind, _ = strconv.Atoi(fields[0])
	ahead, _ = strconv.Atoi(fields[1])
	return behind, ahead
}

func defaultBranch(ctx context.Context, path, current string) string {
	if ref, err := runGit(ctx, path, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if _, branch, ok := strings.Cut(ref, "/"); ok {
			return branch
		}
		return ref
	}

	for _, branch := range []string{"main", "master"} {
		if _, err := runGit(ctx, path, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+branch); err == nil {
			return branch
		}
		if _, err := runGit(ctx, path, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
			return branch
		}
	}

	return current
}

// runGit runs a git command in dir and returns its combined output, trimmed of
// surrounding whitespace.
func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	return runGitTrim(ctx, dir, strings.TrimSpace, args...)
}

// runGitRaw is like runGit but preserves leading whitespace (only trailing
// newlines are trimmed), for output whose first column is significant, e.g.
// porcelain-style status lines where the leading status code matters.
func runGitRaw(ctx context.Context, dir string, args ...string) (string, error) {
	return runGitTrim(ctx, dir, func(s string) string { return strings.TrimRight(s, "\r\n") }, args...)
}

// runGitTrim runs git -C dir args... and applies trim to successful output;
// errors carry the trimmed stderr (or the wrapped error when stderr is empty).
func runGitTrim(ctx context.Context, dir string, trim func(string) string, args ...string) (string, error) {
	if dir == "" {
		return "", errors.New("missing repository path")
	}
	cmdArgs := append([]string{"-C", dir}, args...)
	out, err := exec.CommandContext(ctx, "git", cmdArgs...).CombinedOutput()
	if err != nil {
		if text := strings.TrimSpace(string(out)); text != "" {
			return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), text)
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return trim(string(out)), nil
}
