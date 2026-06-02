package claude

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// TouchedFile is one file a Claude session read or edited inside a repo, with how
// many times and when it was last touched.
type TouchedFile struct {
	Path   string // relative to the repo root
	Reads  int
	Writes int
	Last   time.Time
}

// Wrote reports whether Claude edited (not just read) the file.
func (t TouchedFile) Wrote() bool { return t.Writes > 0 }

// Touches is the total number of read/edit tool calls against the file.
func (t TouchedFile) Touches() int { return t.Reads + t.Writes }

// touchLine is the minimal slice of a transcript line we parse: the tool_use
// blocks and the file path each one carries (Bash and other tools carry none).
type touchLine struct {
	Message struct {
		Content []struct {
			Type  string `json:"type"`
			Name  string `json:"name"`
			Input struct {
				FilePath     string `json:"file_path"`
				NotebookPath string `json:"notebook_path"`
			} `json:"input"`
		} `json:"content"`
	} `json:"message"`
}

// writeTools are the tool names that change a file (everything else is a read).
var writeTools = map[string]bool{
	"Edit": true, "Write": true, "MultiEdit": true, "NotebookEdit": true,
}

// TouchMap scans the newest `limit` transcripts for a repo and returns the files
// Claude read or edited there, scoped to the repo root and sorted edits-first.
func TouchMap(repoPath string, limit int) []TouchedFile {
	repoPath = filepath.Clean(repoPath)
	prefix := repoPath + string(os.PathSeparator)

	type meta struct {
		path string
		mod  time.Time
	}
	var files []meta
	for _, dir := range matchingDirs(repoPath) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			if info, err := e.Info(); err == nil {
				files = append(files, meta{filepath.Join(dir, e.Name()), info.ModTime()})
			}
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })
	if limit > 0 && len(files) > limit {
		files = files[:limit]
	}

	agg := map[string]*TouchedFile{}
	for _, f := range files {
		scanTouches(f.path, f.mod, repoPath, prefix, agg)
	}

	out := make([]TouchedFile, 0, len(agg))
	for _, t := range agg {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Wrote() != b.Wrote() {
			return a.Wrote() // edited files first
		}
		if a.Touches() != b.Touches() {
			return a.Touches() > b.Touches()
		}
		if !a.Last.Equal(b.Last) {
			return a.Last.After(b.Last)
		}
		return a.Path < b.Path
	})
	return out
}

// scanTouches reads one transcript, recording each in-repo file a tool_use block
// touched into agg. mod is the session's mtime, used as the touch time.
func scanTouches(path string, mod time.Time, repoPath, prefix string, agg map[string]*TouchedFile) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

	r := bufio.NewReader(file)
	for {
		line, over, err := readCappedLine(r)
		if !over && strings.Contains(line, `"tool_use"`) {
			var tl touchLine
			if json.Unmarshal([]byte(line), &tl) == nil {
				for _, c := range tl.Message.Content {
					if c.Type != "tool_use" {
						continue
					}
					p := c.Input.FilePath
					if p == "" {
						p = c.Input.NotebookPath
					}
					rel, ok := repoRel(p, repoPath, prefix)
					if !ok {
						continue
					}
					t := agg[rel]
					if t == nil {
						t = &TouchedFile{Path: rel}
						agg[rel] = t
					}
					if writeTools[c.Name] {
						t.Writes++
					} else {
						t.Reads++
					}
					if mod.After(t.Last) {
						t.Last = mod
					}
				}
			}
		}
		if err != nil {
			break
		}
	}
}

// repoRel turns a tool_use file path into a repo-relative path, reporting false
// when the path is empty or points outside the repo (those are noise here).
func repoRel(p, repoPath, prefix string) (string, bool) {
	if p == "" {
		return "", false
	}
	if strings.HasPrefix(p, prefix) {
		return p[len(prefix):], true
	}
	if p == repoPath {
		return filepath.Base(repoPath), true
	}
	if !filepath.IsAbs(p) {
		return p, true // already relative to the launch cwd
	}
	return "", false
}
