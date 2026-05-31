// Package search runs a regex/literal code search across many repositories at
// once. It is implemented entirely in Go - no external tools (ripgrep, grep)
// are required; everything ships in the binary.
package search

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
)

// Target is a repo to search.
type Target struct {
	Name string
	Path string
}

// Match is a single hit.
type Match struct {
	Repo string
	Path string // repo path
	File string // path relative to repo
	Line int
	Text string
}

// Result groups matches under one repo.
type Result struct {
	Repo    string
	Path    string
	Matches []Match
}

// Engine names the backend (always built-in now).
func Engine() string { return "built-in" }

const (
	maxFileSize = 1 << 20 // skip files larger than 1 MiB
	defaultCap  = 200
)

// skipExt are noisy non-source extensions to ignore even when tracked.
var skipExt = map[string]bool{
	".log": true, ".lock": true, ".sum": true, ".map": true, ".min.js": true,
	".svg": true, ".pdf": true, ".ico": true, ".woff": true, ".woff2": true,
}

// Compile turns a user query into a matcher. Regex by default (like ripgrep);
// smart-case (case-insensitive unless the query has uppercase); falls back to a
// literal match if the regex is invalid.
func Compile(query string) (*regexp.Regexp, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	prefix := ""
	if query == strings.ToLower(query) {
		prefix = "(?i)"
	}
	if re, err := regexp.Compile(prefix + query); err == nil {
		return re, nil
	}
	return regexp.Compile(prefix + regexp.QuoteMeta(query))
}

// Search runs the query across all targets. File enumeration is parallel per
// repo; file scanning is a global worker pool across all CPU cores, so one huge
// repo can't bottleneck the whole search.
func Search(ctx context.Context, targets []Target, query string, perRepoCap int) []Result {
	re, err := Compile(query)
	if err != nil || re == nil {
		return nil
	}
	if perRepoCap <= 0 {
		perRepoCap = defaultCap
	}

	// 1. enumerate files for every repo (parallel)
	fileSets := make([][]string, len(targets))
	var fwg sync.WaitGroup
	fsem := make(chan struct{}, 8)
	for i, t := range targets {
		fwg.Add(1)
		go func(i int, t Target) {
			defer fwg.Done()
			fsem <- struct{}{}
			defer func() { <-fsem }()
			fileSets[i] = trackedFiles(ctx, t.Path)
		}(i, t)
	}
	fwg.Wait()

	// 2. scan every file on a global worker pool
	type job struct {
		ri   int
		file string
	}
	res := make([][]Match, len(targets))
	var mu sync.Mutex
	jobCh := make(chan job, 256)

	workers := runtime.NumCPU()
	if workers < 2 {
		workers = 2
	}
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobCh {
				if ctx.Err() != nil {
					continue
				}
				mu.Lock()
				full := len(res[j.ri]) >= perRepoCap
				mu.Unlock()
				if full {
					continue
				}
				ms := scanFile(targets[j.ri], j.file, re, perRepoCap)
				if len(ms) == 0 {
					continue
				}
				mu.Lock()
				if room := perRepoCap - len(res[j.ri]); room > 0 {
					if len(ms) > room {
						ms = ms[:room]
					}
					res[j.ri] = append(res[j.ri], ms...)
				}
				mu.Unlock()
			}
		}()
	}
	for ri := range targets {
		for _, f := range fileSets[ri] {
			if skipExt[strings.ToLower(filepath.Ext(f))] {
				continue
			}
			if ctx.Err() != nil {
				break
			}
			jobCh <- job{ri, f}
		}
	}
	close(jobCh)
	wg.Wait()

	// 3. assemble in repo order
	out := make([]Result, 0, len(targets))
	for i, t := range targets {
		if len(res[i]) > 0 {
			out = append(out, Result{Repo: t.Name, Path: t.Path, Matches: res[i]})
		}
	}
	return out
}

func scanFile(t Target, rel string, re *regexp.Regexp, cap int) []Match {
	full := filepath.Join(t.Path, rel)
	info, err := os.Stat(full)
	if err != nil || info.Size() == 0 || info.Size() > maxFileSize {
		return nil
	}
	data, err := os.ReadFile(full)
	if err != nil || isBinary(data) || !re.Match(data) { // whole-file pre-check
		return nil
	}
	var ms []Match
	for n, line := range strings.Split(string(data), "\n") {
		if re.MatchString(line) {
			line = strings.TrimSpace(line)
			if len(line) > 200 {
				line = line[:200]
			}
			ms = append(ms, Match{Repo: t.Name, Path: t.Path, File: rel, Line: n + 1, Text: line})
			if len(ms) >= cap {
				break
			}
		}
	}
	return ms
}

// trackedFiles lists git-tracked and untracked-but-not-ignored files. Using git
// here (already required by orchard) means .gitignore is honored for free and we
// never crawl node_modules/build output - the difference between fast and slow.
func trackedFiles(ctx context.Context, dir string) []string {
	cmd := exec.CommandContext(ctx, "git", "-C", dir,
		"ls-files", "--cached", "--others", "--exclude-standard", "-z")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	parts := strings.Split(strings.TrimRight(string(out), "\x00"), "\x00")
	files := parts[:0]
	for _, p := range parts {
		if p != "" {
			files = append(files, p)
		}
	}
	return files
}

// isBinary reports whether data looks non-text (contains a NUL in the head).
func isBinary(data []byte) bool {
	n := len(data)
	if n > 512 {
		n = 512
	}
	return bytes.IndexByte(data[:n], 0) >= 0
}
