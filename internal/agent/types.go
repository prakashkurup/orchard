// Package agent holds the provider-neutral data types that describe a coding
// agent's local footprint in a repo: its sessions, usage rollup, the files it
// touched, and content-search hits. Both the claude and codex readers produce
// these, so the TUI renders either the same way.
package agent

import (
	"strings"
	"time"
)

// Session is a single agent session/transcript.
type Session struct {
	ID        string
	Modified  time.Time
	Model     string
	Assistant int    // count of assistant turns
	Title     string // the session's title, or a fallback derived from the first prompt
	Tokens    int    // total tokens across the session
}

// DisplayTitle is a human label for the session: its title, else a short id.
func (s Session) DisplayTitle() string {
	switch {
	case strings.TrimSpace(s.Title) != "":
		return s.Title
	case len(s.ID) >= 8:
		return "session " + s.ID[:8]
	default:
		return "session"
	}
}

// Target identifies a repo for aggregation.
type Target struct {
	Name string
	Path string
}

// RepoUsage is one repo's agent footprint.
type RepoUsage struct {
	Name     string
	Path     string
	Sessions int
	Turns    int
	Tokens   int
	Last     time.Time
}

// Usage is the global agent rollup across repos.
type Usage struct {
	TotalSessions int
	TotalTurns    int
	TotalTokens   int
	ReposUsed     int
	Models        map[string]int // pretty model -> assistant turns
	Repos         []RepoUsage    // sorted by turns desc
	Last          time.Time
}

// TouchedFile is one file an agent read or edited inside a repo, with how many
// times and when it was last touched.
type TouchedFile struct {
	Path   string // relative to the repo root
	Reads  int
	Writes int
	Last   time.Time
}

// Wrote reports whether the agent edited (not just read) the file.
func (t TouchedFile) Wrote() bool { return t.Writes > 0 }

// Touches is the total number of read/edit operations against the file.
func (t TouchedFile) Touches() int { return t.Reads + t.Writes }

// SessionHit is a session whose content matched a search query.
type SessionHit struct {
	RepoName string
	RepoPath string
	ID       string
	Title    string
	Snippet  string // cleaned text around the first match
	Modified time.Time
}

// DisplayTitle is a human label for the hit: its title, else a short session id.
func (h SessionHit) DisplayTitle() string {
	if strings.TrimSpace(h.Title) != "" {
		return h.Title
	}
	if len(h.ID) >= 8 {
		return "session " + h.ID[:8]
	}
	return "session"
}
