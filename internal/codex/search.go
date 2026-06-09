package codex

import (
	"bufio"
	"encoding/json"
	"os"
	"sort"
	"strings"

	"github.com/prakashkurup/orchard/internal/agent"
)

// SearchSessions does a case-insensitive substring search over the message text
// of Codex sessions in the given repos and returns matches with a snippet,
// newest first, capped at limit.
func SearchSessions(targets []agent.Target, query string, limit int) []agent.SessionHit {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}
	_, titles := index()
	var hits []agent.SessionHit
	for _, t := range targets {
		for _, r := range refsFor(t.Path) {
			if snip, ok := searchFile(r.path, q); ok {
				hits = append(hits, agent.SessionHit{
					RepoName: t.Name,
					RepoPath: t.Path,
					ID:       r.id,
					Title:    titles[r.id],
					Snippet:  snip,
					Modified: r.mod,
				})
			}
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Modified.After(hits[j].Modified) })
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

// searchFile returns a snippet around the first match of q within a message's
// text content (user/assistant), or ok=false if the session has no match.
func searchFile(path, q string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	r := bufio.NewReader(f)
	for {
		line, over, err := readCappedLine(r)
		if !over && strings.Contains(line, `"message"`) {
			for _, txt := range messageTexts(line) {
				low := strings.ToLower(txt)
				if i := strings.Index(low, q); i >= 0 {
					return snippet(txt, len([]rune(low[:i])), len([]rune(q))), true
				}
			}
		}
		if err != nil {
			break
		}
	}
	return "", false
}

// messageTexts pulls the text blocks out of a response_item message line.
func messageTexts(line string) []string {
	var rec struct {
		Type    string `json:"type"`
		Payload struct {
			Type    string `json:"type"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"payload"`
	}
	if json.Unmarshal([]byte(line), &rec) != nil || rec.Type != "response_item" || rec.Payload.Type != "message" {
		return nil
	}
	out := make([]string, 0, len(rec.Payload.Content))
	for _, c := range rec.Payload.Content {
		if c.Text != "" {
			out = append(out, c.Text)
		}
	}
	return out
}

// snippet returns up to ~48 runes on each side of a match (given as a rune
// index), flattened to a single line.
func snippet(text string, runeAt, qlen int) string {
	const pad = 48
	runes := []rune(text)
	if runeAt > len(runes) {
		runeAt = len(runes)
	}
	lo := runeAt - pad
	if lo < 0 {
		lo = 0
	}
	hi := runeAt + qlen + pad
	if hi > len(runes) {
		hi = len(runes)
	}
	s := strings.TrimSpace(strings.ReplaceAll(string(runes[lo:hi]), "\n", " "))
	if lo > 0 {
		s = "…" + s
	}
	if hi < len(runes) {
		s = s + "…"
	}
	return s
}
