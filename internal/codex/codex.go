// Package codex reads local Codex CLI session rollouts to surface, per
// repository, how much it has been worked on with Codex: number of sessions,
// when it was last used, which model, tokens, and the files it changed.
//
// Codex stores one rollout JSONL per session under
// $CODEX_HOME/sessions/YYYY/MM/DD/rollout-<ts>-<uuid>.jsonl (default ~/.codex).
// Unlike Claude, sessions are foldered by date, not by repo: the repo a session
// belongs to is its session_meta `cwd`, so we index every rollout's first line
// once and group by cwd.
package codex

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/prakashkurup/orchard/internal/agent"
)

// Home is $CODEX_HOME, else ~/.codex.
func Home() string {
	if dir := strings.TrimSpace(os.Getenv("CODEX_HOME")); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex")
}

func sessionsDir() string {
	if h := Home(); h != "" {
		return filepath.Join(h, "sessions")
	}
	return ""
}

// sessionRef is the cheap, first-line view of a rollout: which repo it ran in
// (cwd) and when (file mtime), without parsing the whole conversation.
type sessionRef struct {
	id   string
	cwd  string
	path string
	mod  time.Time
}

// the rollout index is cached briefly so a dashboard scan (which asks for every
// repo's summary in quick succession) builds it once, not once per repo.
var (
	idxMu     sync.Mutex
	idxAt     time.Time
	idxRoot   string
	idxRefs   []sessionRef
	idxTitles map[string]string
	hasBuilt  bool
	idxTTL    = 2 * time.Second
)

func index() ([]sessionRef, map[string]string) {
	root := sessionsDir()
	idxMu.Lock()
	defer idxMu.Unlock()
	if hasBuilt && idxRoot == root && time.Since(idxAt) < idxTTL {
		return idxRefs, idxTitles
	}
	idxRefs = buildIndex()
	idxTitles = loadTitles()
	idxAt = time.Now()
	idxRoot = root
	hasBuilt = true
	return idxRefs, idxTitles
}

func buildIndex() []sessionRef {
	root := sessionsDir()
	if root == "" {
		return nil
	}
	var refs []sessionRef
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasPrefix(name, "rollout-") || !strings.HasSuffix(name, ".jsonl") {
			return nil
		}
		id, cwd := metaFromFirstLine(path)
		if id == "" {
			return nil
		}
		mod := time.Time{}
		if info, ierr := d.Info(); ierr == nil {
			mod = info.ModTime()
		}
		refs = append(refs, sessionRef{id: id, cwd: cwd, path: path, mod: mod})
		return nil
	})
	return refs
}

// loadTitles maps session id -> thread_name from session_index.jsonl (Codex's
// own index of recent sessions; not every session is listed).
func loadTitles() map[string]string {
	out := map[string]string{}
	h := Home()
	if h == "" {
		return out
	}
	f, err := os.Open(filepath.Join(h, "session_index.jsonl"))
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		var e struct {
			ID         string `json:"id"`
			ThreadName string `json:"thread_name"`
		}
		if json.Unmarshal(sc.Bytes(), &e) == nil && e.ID != "" {
			out[e.ID] = e.ThreadName
		}
	}
	return out
}

// metaFromFirstLine reads the session_meta (first line) for the session id and
// the cwd it ran in.
func metaFromFirstLine(path string) (id, cwd string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	r := bufio.NewReader(f)
	line, _, _ := readCappedLine(r)
	var rec struct {
		Type    string `json:"type"`
		Payload struct {
			ID  string `json:"id"`
			Cwd string `json:"cwd"`
		} `json:"payload"`
	}
	if json.Unmarshal([]byte(line), &rec) != nil || rec.Type != "session_meta" {
		return "", ""
	}
	return rec.Payload.ID, rec.Payload.Cwd
}

// inRepo reports whether a session's cwd belongs to repoPath (the repo root or a
// directory inside it).
func inRepo(cwd, repoPath string) bool {
	if cwd == "" {
		return false
	}
	cwd = filepath.Clean(cwd)
	repoPath = filepath.Clean(repoPath)
	return cwd == repoPath || strings.HasPrefix(cwd, repoPath+string(os.PathSeparator))
}

func refsFor(repoPath string) []sessionRef {
	refs, _ := index()
	var out []sessionRef
	for _, r := range refs {
		if inRepo(r.cwd, repoPath) {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].mod.After(out[j].mod) })
	return out
}

// Summary is the cheap view: how many Codex sessions ran in the repo and when
// the most recent one was. Safe to call for every repo during a scan.
func Summary(repoPath string) (sessions int, last time.Time) {
	for _, r := range refsFor(repoPath) {
		sessions++
		if r.mod.After(last) {
			last = r.mod
		}
	}
	return sessions, last
}

// Sessions parses the newest `limit` rollouts for a repo (model, turns, tokens,
// title). More expensive than Summary; for the detail view and aggregation.
func Sessions(repoPath string, limit int) []agent.Session {
	refs := refsFor(repoPath)
	if limit > 0 && len(refs) > limit {
		refs = refs[:limit]
	}
	_, titles := index()
	out := make([]agent.Session, 0, len(refs))
	for _, r := range refs {
		s := agent.Session{ID: r.id, Modified: r.mod, Title: titles[r.id]}
		parseSession(r.path, &s)
		out = append(out, s)
	}
	return out
}

// Aggregate rolls up Codex usage across the given repos. Builds the index once.
func Aggregate(targets []agent.Target) agent.Usage {
	u := agent.Usage{Models: map[string]int{}}
	for _, t := range targets {
		sessions := Sessions(t.Path, 0)
		if len(sessions) == 0 {
			continue
		}
		ru := agent.RepoUsage{Name: t.Name, Path: t.Path}
		for _, s := range sessions {
			ru.Sessions++
			ru.Turns += s.Assistant
			ru.Tokens += s.Tokens
			if s.Modified.After(ru.Last) {
				ru.Last = s.Modified
			}
			if m := PrettyModel(s.Model); m != "" {
				u.Models[m] += s.Assistant
			}
		}
		u.TotalSessions += ru.Sessions
		u.TotalTurns += ru.Turns
		u.TotalTokens += ru.Tokens
		u.ReposUsed++
		if ru.Last.After(u.Last) {
			u.Last = ru.Last
		}
		u.Repos = append(u.Repos, ru)
	}
	sort.Slice(u.Repos, func(i, j int) bool {
		if u.Repos[i].Turns != u.Repos[j].Turns {
			return u.Repos[i].Turns > u.Repos[j].Turns
		}
		return u.Repos[i].Sessions > u.Repos[j].Sessions
	})
	return u
}

// parseSession reads one rollout for assistant-turn count, total tokens, model,
// and a title fallback (the first user message) when Codex listed none.
func parseSession(path string, s *agent.Session) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	needTitle := strings.TrimSpace(s.Title) == ""
	r := bufio.NewReader(f)
	for {
		line, over, err := readCappedLine(r)
		if line != "" && !over {
			switch {
			case strings.Contains(line, `"agent_message"`):
				if t := eventType(line); t == "agent_message" {
					s.Assistant++
				}
			case strings.Contains(line, `"token_count"`):
				if n := totalTokens(line); n > s.Tokens {
					s.Tokens = n // total_token_usage is cumulative; the last/largest wins
				}
			case strings.Contains(line, `"turn_context"`):
				if m := turnModel(line); m != "" {
					s.Model = m
				}
			case needTitle && strings.Contains(line, `"role":"user"`):
				if txt := firstUserText(line); txt != "" {
					s.Title = firstLine(txt)
					needTitle = false
				}
			}
		}
		if err != nil {
			break
		}
	}
}

func eventType(line string) string {
	var rec struct {
		Type    string `json:"type"`
		Payload struct {
			Type string `json:"type"`
		} `json:"payload"`
	}
	if json.Unmarshal([]byte(line), &rec) == nil && rec.Type == "event_msg" {
		return rec.Payload.Type
	}
	return ""
}

func totalTokens(line string) int {
	var rec struct {
		Payload struct {
			Type string `json:"type"`
			Info struct {
				Total struct {
					TotalTokens int `json:"total_tokens"`
				} `json:"total_token_usage"`
			} `json:"info"`
		} `json:"payload"`
	}
	if json.Unmarshal([]byte(line), &rec) == nil && rec.Payload.Type == "token_count" {
		return rec.Payload.Info.Total.TotalTokens
	}
	return 0
}

func turnModel(line string) string {
	var rec struct {
		Type    string `json:"type"`
		Payload struct {
			Model string `json:"model"`
		} `json:"payload"`
	}
	if json.Unmarshal([]byte(line), &rec) == nil && rec.Type == "turn_context" {
		return rec.Payload.Model
	}
	return ""
}

func firstUserText(line string) string {
	var rec struct {
		Payload struct {
			Type    string `json:"type"`
			Role    string `json:"role"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"payload"`
	}
	if json.Unmarshal([]byte(line), &rec) != nil {
		return ""
	}
	if rec.Payload.Type != "message" || rec.Payload.Role != "user" {
		return ""
	}
	for _, c := range rec.Payload.Content {
		if strings.TrimSpace(c.Text) != "" {
			return c.Text
		}
	}
	return ""
}

func firstLine(s string) string {
	for _, ln := range strings.Split(s, "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			return ln
		}
	}
	return ""
}

// PrettyModel shortens a Codex model id for display (e.g. trims a provider
// prefix). Codex ids are already short like "gpt-5.5", so this is light.
func PrettyModel(model string) string {
	m := strings.TrimSpace(model)
	if i := strings.LastIndex(m, "/"); i >= 0 { // strip any "provider/model" prefix
		m = m[i+1:]
	}
	return m
}

// maxRolloutLine caps how much of one JSONL line is buffered: tool output and
// images can be enormous, but the fields read here are all small.
const maxRolloutLine = 4 << 20

// readCappedLine reads the next line, accumulating at most maxRolloutLine bytes,
// returning the (possibly capped) line, over=true when it was truncated, and the
// read error (io.EOF on the last line).
func readCappedLine(r *bufio.Reader) (string, bool, error) {
	var b []byte
	over := false
	for {
		chunk, err := r.ReadSlice('\n')
		if len(b) < maxRolloutLine {
			if room := maxRolloutLine - len(b); len(chunk) > room {
				b = append(b, chunk[:room]...)
				over = true
			} else {
				b = append(b, chunk...)
			}
		} else if len(chunk) > 0 {
			over = true
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		return string(b), over, err
	}
}
