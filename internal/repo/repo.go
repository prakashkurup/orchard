// Package repo defines the Repo model, its derived display state, and discovery
// of local git repositories under a root directory.
package repo

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DisplayState is a repo's summarized status for display (clean, dirty, behind…).
type DisplayState int

const (
	DisplayClean DisplayState = iota
	DisplayDirty
	DisplayBehind
	DisplayAhead
	DisplayDiverged
	DisplayFeature
	DisplayDetached
	DisplayNoUpstream
	DisplayError
)

func (s DisplayState) String() string {
	switch s {
	case DisplayClean:
		return "clean"
	case DisplayDirty:
		return "dirty"
	case DisplayBehind:
		return "behind"
	case DisplayAhead:
		return "ahead"
	case DisplayDiverged:
		return "diverged"
	case DisplayFeature:
		return "feature"
	case DisplayDetached:
		return "detached"
	case DisplayNoUpstream:
		return "no-upstream"
	case DisplayError:
		return "error"
	default:
		return "unknown"
	}
}

func (s DisplayState) Glyph() string {
	switch s {
	case DisplayClean:
		return "✓"
	case DisplayDirty:
		return "!"
	case DisplayBehind:
		return "↓"
	case DisplayAhead:
		return "↑"
	case DisplayDiverged:
		return "↕"
	case DisplayFeature:
		return "⎇"
	case DisplayDetached:
		return "⌁"
	case DisplayNoUpstream:
		return "?"
	case DisplayError:
		return "×"
	default:
		return "?"
	}
}

// Repo is a discovered local repository and its derived git state.
type Repo struct {
	Name          string       `json:"name"`
	Path          string       `json:"path"`
	Branch        string       `json:"branch"`
	DefaultBranch string       `json:"default_branch"`
	Head          string       `json:"head,omitempty"` // current HEAD sha (for since-last-visit)
	Upstream      string       `json:"upstream,omitempty"`
	Dirty         bool         `json:"dirty"`
	Ahead         int          `json:"ahead"`
	Behind        int          `json:"behind"`
	HasUpstream   bool         `json:"has_upstream"`
	OnDefault     bool         `json:"on_default"`
	Detached      bool         `json:"detached"`
	LastCommit    string       `json:"last_commit"`
	LastFetched   time.Time    `json:"last_fetched,omitempty"`
	JustPulled    bool         `json:"-"`
	ChangedFiles  int          `json:"changed_files,omitempty"`
	Stashes       int          `json:"stashes,omitempty"`
	CCSessions    int          `json:"cc_sessions,omitempty"`
	CCLast        time.Time    `json:"cc_last,omitempty"`
	Activity      []int        `json:"-"` // weekly commit counts, oldest first, for the sparkline
	Display       DisplayState `json:"display"`
	SkipReason    string       `json:"skip_reason,omitempty"`
	Err           string       `json:"error,omitempty"`
}

// ComputeDisplay derives a repo's DisplayState from its git fields.
func ComputeDisplay(r Repo) DisplayState {
	switch {
	case r.Err != "":
		return DisplayError
	case r.Detached:
		return DisplayDetached
	case r.Dirty:
		return DisplayDirty
	case r.Ahead > 0 && r.Behind > 0:
		return DisplayDiverged
	case r.Behind > 0:
		return DisplayBehind
	case r.Ahead > 0:
		return DisplayAhead
	case r.Branch != "" && r.DefaultBranch != "" && !r.OnDefault:
		return DisplayFeature
	case !r.HasUpstream:
		return DisplayNoUpstream
	default:
		return DisplayClean
	}
}

func (r Repo) WithDisplay() Repo {
	r.OnDefault = !r.Detached && r.Branch != "" && r.DefaultBranch != "" && r.Branch == r.DefaultBranch
	r.Display = ComputeDisplay(r)
	return r
}

func (r Repo) PullSkipReason() string {
	switch {
	case r.Err != "":
		return "status error: " + r.Err
	case r.Detached:
		return "detached HEAD"
	case r.Dirty:
		return "working tree is dirty"
	case r.DefaultBranch != "" && !r.OnDefault:
		return "on non-default branch " + r.Branch
	case !r.HasUpstream:
		return "no upstream configured"
	default:
		return ""
	}
}

// ExpandPath expands a leading ~ or ~/ to the user's home directory.
func ExpandPath(path string) string {
	if path == "" {
		return path
	}
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// Discover finds git repositories directly under root (one level deep).
func Discover(root string) ([]Repo, error) {
	root = ExpandPath(root)
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	if isGitRepo(absRoot) {
		return []Repo{{
			Name: filepath.Base(absRoot),
			Path: absRoot,
		}}, nil
	}

	entries, err := os.ReadDir(absRoot)
	if err != nil {
		return nil, err
	}

	repos := make([]Repo, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		path := filepath.Join(absRoot, name)
		if isGitRepo(path) {
			repos = append(repos, Repo{Name: name, Path: path})
		}
	}

	sort.Slice(repos, func(i, j int) bool {
		return strings.ToLower(repos[i].Name) < strings.ToLower(repos[j].Name)
	})
	return repos, nil
}

func isGitRepo(path string) bool {
	if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
		return true
	}
	return false
}
