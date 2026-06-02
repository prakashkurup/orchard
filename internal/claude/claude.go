// Package claude reads local Claude Code session transcripts to surface, per
// repository, how much it has been worked on with Claude Code: number of
// sessions, when it was last used, and which model.
//
// Claude Code stores transcripts under ~/.claude/projects/<encoded-path>/,
// where the path is the repo's absolute path with "/" and "." replaced by "-".
package claude

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Session is a single Claude Code transcript.
type Session struct {
	ID        string
	Modified  time.Time
	Model     string
	Assistant int    // count of assistant turns
	Title     string // Claude's "ai-title" summary, or a fallback from the last prompt
	Tokens    int    // total tokens (input + output + cache) across the session
}

// DisplayTitle is a human label for the session: its ai-title, else a trimmed
// last prompt, else a short form of the session id.
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

func encode(p string) string {
	return strings.NewReplacer("/", "-", ".", "-").Replace(p)
}

// ProjectsRoot is the transcript directory: $CLAUDE_CONFIG_DIR/projects when that
// env var relocates Claude's config, else ~/.claude/projects.
func ProjectsRoot() string {
	if dir := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); dir != "" {
		return filepath.Join(dir, "projects")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "projects")
}

// DirFor returns the transcript directory for a repo path.
func DirFor(repoPath string) string {
	root := ProjectsRoot()
	if root == "" {
		return ""
	}
	return filepath.Join(root, encode(repoPath))
}

// matchingDirs returns every transcript dir belonging to a repo: the repo's own
// cwd dir, plus any subdirectory cwds (Claude Code launched from within the
// repo, e.g. a subfolder or worktree). Claude keys projects by the launch cwd,
// so a session started in <repo>/app lives under a different dir - without this
// it would be invisible in the per-repo rollup.
func matchingDirs(repoPath string) []string {
	root := ProjectsRoot()
	if root == "" {
		return nil
	}
	enc := encode(repoPath)
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var dirs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if n := e.Name(); n == enc || strings.HasPrefix(n, enc+"-") {
			dirs = append(dirs, filepath.Join(root, n))
		}
	}
	return dirs
}

// Summary is the cheap, filesystem-only view: how many sessions and the most
// recent modification time. Safe to call for every repo during a scan.
func Summary(repoPath string) (sessions int, last time.Time) {
	for _, dir := range matchingDirs(repoPath) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			sessions++
			if info.ModTime().After(last) {
				last = info.ModTime()
			}
		}
	}
	return sessions, last
}

// Sessions parses the newest `limit` transcripts (model + assistant-turn count).
// More expensive than Summary; intended for the on-demand detail view.
func Sessions(repoPath string, limit int) []Session {
	type fileMeta struct {
		path string
		name string
		mod  time.Time
	}
	var files []fileMeta
	for _, dir := range matchingDirs(repoPath) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			files = append(files, fileMeta{filepath.Join(dir, e.Name()), e.Name(), info.ModTime()})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })
	if limit > 0 && len(files) > limit {
		files = files[:limit]
	}

	out := make([]Session, 0, len(files))
	for _, f := range files {
		s := Session{ID: strings.TrimSuffix(f.name, ".jsonl"), Modified: f.mod}
		parseSession(f.path, &s)
		out = append(out, s)
	}
	return out
}

func parseSession(path string, s *Session) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

	var lastPrompt string
	r := bufio.NewReader(file)
	for {
		line, err := r.ReadString('\n') // handles arbitrarily long lines (base64 images, etc.)
		if line != "" {
			if strings.Contains(line, `"type":"assistant"`) {
				s.Assistant++
			}
			if m := extract(line, `"model":"`); m != "" && m != "<synthetic>" {
				s.Model = m
			}
			// token usage lives on assistant lines; the contains-guard keeps the
			// (cost-bearing) integer scans off lines that have no usage block.
			if strings.Contains(line, `"output_tokens":`) {
				s.Tokens += extractInt(line, `"input_tokens":`) +
					extractInt(line, `"output_tokens":`) +
					extractInt(line, `"cache_creation_input_tokens":`) +
					extractInt(line, `"cache_read_input_tokens":`)
			}
			// The title and last-prompt lines are small; JSON-parse them so titles
			// containing quotes or escapes survive intact (the cheap string scan
			// above can't). Other (possibly huge) lines are never parsed.
			if strings.Contains(line, `"type":"ai-title"`) {
				var t struct {
					AiTitle string `json:"aiTitle"`
				}
				if json.Unmarshal([]byte(line), &t) == nil && t.AiTitle != "" {
					s.Title = t.AiTitle
				}
			} else if strings.Contains(line, `"type":"last-prompt"`) {
				var p struct {
					LastPrompt string `json:"lastPrompt"`
				}
				if json.Unmarshal([]byte(line), &p) == nil {
					lastPrompt = p.LastPrompt
				}
			}
		}
		if err != nil {
			break
		}
	}
	if s.Title == "" {
		s.Title = firstLine(lastPrompt)
	}
}

// firstLine returns the first non-empty line of s, trimmed, for use as a fallback
// session title when Claude has not generated an ai-title yet.
func firstLine(s string) string {
	for _, ln := range strings.Split(s, "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			return ln
		}
	}
	return ""
}

// extractInt reads the integer immediately following key in line (0 if absent),
// e.g. extractInt(`"output_tokens": 1234`, `"output_tokens":`) == 1234.
func extractInt(line, key string) int {
	i := strings.Index(line, key)
	if i < 0 {
		return 0
	}
	rest := line[i+len(key):]
	j := 0
	for j < len(rest) && rest[j] == ' ' {
		j++
	}
	k := j
	for k < len(rest) && rest[k] >= '0' && rest[k] <= '9' {
		k++
	}
	if k == j {
		return 0
	}
	n, _ := strconv.Atoi(rest[j:k])
	return n
}

func extract(line, key string) string {
	i := strings.Index(line, key)
	if i < 0 {
		return ""
	}
	rest := line[i+len(key):]
	j := strings.IndexByte(rest, '"')
	if j < 0 {
		return ""
	}
	return rest[:j]
}

// Target identifies a repo for aggregation.
type Target struct {
	Name string
	Path string
}

// RepoUsage is the Claude Code footprint of a single repo.
type RepoUsage struct {
	Name     string
	Path     string
	Sessions int
	Turns    int
	Tokens   int
	Last     time.Time
}

// Usage is the global Claude Code rollup across repos.
type Usage struct {
	TotalSessions int
	TotalTurns    int
	TotalTokens   int
	ReposUsed     int
	Models        map[string]int // pretty model -> assistant turns
	Repos         []RepoUsage    // sorted by turns desc
	Last          time.Time
}

// Aggregate parses every transcript for each target and rolls up totals. It is
// I/O heavy (reads all sessions), so callers should run it off the UI thread.
func Aggregate(targets []Target) Usage {
	u := Usage{Models: map[string]int{}}

	type res struct {
		ru     RepoUsage
		models map[string]int
	}
	results := make(chan res, len(targets))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for _, t := range targets {
		wg.Add(1)
		go func(t Target) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			sessions := Sessions(t.Path, 0)
			r := res{ru: RepoUsage{Name: t.Name, Path: t.Path}, models: map[string]int{}}
			for _, s := range sessions {
				r.ru.Sessions++
				r.ru.Turns += s.Assistant
				r.ru.Tokens += s.Tokens
				if s.Modified.After(r.ru.Last) {
					r.ru.Last = s.Modified
				}
				if m := PrettyModel(s.Model); m != "" {
					r.models[m] += s.Assistant
				}
			}
			results <- r
		}(t)
	}
	go func() { wg.Wait(); close(results) }()

	for r := range results {
		if r.ru.Sessions == 0 {
			continue
		}
		u.TotalSessions += r.ru.Sessions
		u.TotalTurns += r.ru.Turns
		u.TotalTokens += r.ru.Tokens
		u.ReposUsed++
		if r.ru.Last.After(u.Last) {
			u.Last = r.ru.Last
		}
		for m, n := range r.models {
			u.Models[m] += n
		}
		u.Repos = append(u.Repos, r.ru)
	}
	sort.Slice(u.Repos, func(i, j int) bool {
		if u.Repos[i].Turns != u.Repos[j].Turns {
			return u.Repos[i].Turns > u.Repos[j].Turns
		}
		return u.Repos[i].Sessions > u.Repos[j].Sessions
	})
	return u
}

// PrettyModel shortens a model id like "claude-opus-4-8-20251101" to "opus-4.8".
func PrettyModel(model string) string {
	if model == "" {
		return ""
	}
	m := strings.TrimPrefix(model, "claude-")
	// drop trailing date stamp (a long digit run)
	parts := strings.Split(m, "-")
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if len(p) >= 8 && isAllDigits(p) {
			continue
		}
		kept = append(kept, p)
	}
	m = strings.Join(kept, "-")
	// "opus-4-8" -> "opus-4.8"
	if i := strings.LastIndex(m, "-"); i > 0 {
		tail := m[i+1:]
		if len(tail) == 1 && tail[0] >= '0' && tail[0] <= '9' {
			head := m[:i]
			if j := strings.LastIndex(head, "-"); j > 0 && isAllDigits(head[j+1:]) {
				m = head[:j] + "-" + head[j+1:] + "." + tail
			}
		}
	}
	return m
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
