package claude

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// SessionHit is a transcript whose content matched a search query.
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

// SearchSessions scans every transcript for the targets and returns those whose
// content contains query (case-insensitive), newest first, capped at limit.
func SearchSessions(targets []Target, query string, limit int) []SessionHit {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}
	var (
		mu   sync.Mutex
		hits []SessionHit
	)
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for _, t := range targets {
		wg.Add(1)
		go func(t Target) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			var local []SessionHit
			for _, dir := range matchingDirs(t.Path) {
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
					h, ok := searchFile(filepath.Join(dir, e.Name()), q)
					if !ok {
						continue
					}
					h.RepoName, h.RepoPath = t.Name, t.Path
					h.ID = strings.TrimSuffix(e.Name(), ".jsonl")
					h.Modified = info.ModTime()
					local = append(local, h)
				}
			}
			if len(local) > 0 {
				mu.Lock()
				hits = append(hits, local...)
				mu.Unlock()
			}
		}(t)
	}
	wg.Wait()
	sort.Slice(hits, func(i, j int) bool { return hits[i].Modified.After(hits[j].Modified) })
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

func searchFile(path, q string) (SessionHit, bool) {
	file, err := os.Open(path)
	if err != nil {
		return SessionHit{}, false
	}
	defer file.Close()

	var (
		h          SessionHit
		matched    bool
		lastPrompt string
	)
	r := bufio.NewReader(file)
	for {
		line, err := r.ReadString('\n')
		if line != "" {
			if !matched {
				if idx := strings.Index(strings.ToLower(line), q); idx >= 0 {
					matched = true
					h.Snippet = snippetAround(line, idx, len(q))
				}
			}
			if strings.Contains(line, `"type":"ai-title"`) {
				var t struct {
					AiTitle string `json:"aiTitle"`
				}
				if json.Unmarshal([]byte(line), &t) == nil && t.AiTitle != "" {
					h.Title = t.AiTitle
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
	if !matched {
		return SessionHit{}, false
	}
	if h.Title == "" {
		h.Title = firstLine(lastPrompt)
	}
	return h, true
}

func snippetAround(line string, idx, qlen int) string {
	const pad = 48
	start, end := idx-pad, idx+qlen+pad
	if start < 0 {
		start = 0
	}
	if end > len(line) {
		end = len(line)
	}
	// snap to rune boundaries so a multibyte char at the edge isn't cut
	for start > 0 && !utf8.RuneStart(line[start]) {
		start--
	}
	for end < len(line) && !utf8.RuneStart(line[end]) {
		end++
	}
	snip := cleanSnippet(line[start:end])
	if start > 0 {
		snip = "…" + snip
	}
	if end < len(line) {
		snip += "…"
	}
	return snip
}

// cleanSnippet unescapes common JSON escapes and strips control bytes (C0, DEL,
// and C1) so a raw transcript line renders as readable, escape-free context.
func cleanSnippet(s string) string {
	s = strings.NewReplacer(`\n`, " ", `\t`, " ", `\"`, `"`, `\\`, `\`).Replace(s)
	var b strings.Builder
	lastSpace := false
	for _, r := range s {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			r = ' '
		}
		if r == ' ' {
			if lastSpace {
				continue
			}
			lastSpace = true
		} else {
			lastSpace = false
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}
