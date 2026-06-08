package graph

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// maxFileBytes caps file size; larger files (usually generated/vendored blobs)
// are skipped rather than parsed.
const maxFileBytes = 2 << 20 // 2 MiB

// extLang maps a lower-case extension (no leading dot) to a language label.
// Only files whose extension appears here are treated as source.
var extLang = map[string]string{
	"go":   "go",
	"py":   "python",
	"rb":   "ruby",
	"rs":   "rust",
	"java": "java",
	"kt":   "kotlin",
	"kts":  "kotlin",
	"cs":   "csharp",
	"ts":   "typescript",
	"tsx":  "tsx",
	"js":   "javascript",
	"jsx":  "javascript",
	"mjs":  "javascript",
	"c":    "c",
	"h":    "c",
	"cc":   "cpp",
	"cpp":  "cpp",
	"cxx":  "cpp",
	"hpp":  "cpp",
	"hh":   "cpp",
}

// DiscoveredFile is one candidate source file together with its contents.
type DiscoveredFile struct {
	Rel  string // repo-relative path
	Lang string // language label (see extLang)
	Data []byte
	SHA  string // sha256 of Data (hex), for incremental reindex later
}

// Discover lists git-tracked source files in repo, skipping ignored (untracked)
// files for free and dropping binary, generated, and oversized files. skipped
// counts files dropped by the binary/generated/oversize filters.
func Discover(ctx context.Context, repo string) (files []DiscoveredFile, skipped int, err error) {
	out, err := exec.CommandContext(ctx, "git", "-C", repo, "ls-files", "-z").Output()
	if err != nil {
		return nil, 0, err
	}
	for _, rel := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if rel == "" {
			continue
		}
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(rel), "."))
		lang, ok := extLang[ext]
		if !ok {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(repo, rel))
		if readErr != nil {
			skipped++
			continue
		}
		if len(data) > maxFileBytes || isBinary(data) || isGenerated(rel, data) {
			skipped++
			continue
		}
		if ext == "h" {
			lang = routeHeaderLang(repo, rel, data)
		}
		sum := sha256.Sum256(data)
		files = append(files, DiscoveredFile{
			Rel: rel, Lang: lang, Data: data, SHA: hex.EncodeToString(sum[:]),
		})
	}
	return files, skipped, nil
}

// isBinary reports whether data looks binary (a NUL byte in the first 8 KiB).
func isBinary(data []byte) bool {
	n := len(data)
	if n > 8000 {
		n = 8000
	}
	return bytes.IndexByte(data[:n], 0) >= 0
}

// isGenerated reports whether a file looks machine-generated (and so noise for
// the graph), by filename suffix or a "Code generated … DO NOT EDIT" header.
func isGenerated(rel string, data []byte) bool {
	base := filepath.Base(rel)
	for _, suf := range []string{".pb.go", ".min.js", "_pb2.py", ".pb.cc", ".pb.h", ".g.dart"} {
		if strings.HasSuffix(base, suf) {
			return true
		}
	}
	head := data
	if len(head) > 1000 {
		head = head[:1000]
	}
	return bytes.Contains(head, []byte("Code generated")) && bytes.Contains(head, []byte("DO NOT EDIT"))
}

// routeHeaderLang sends ambiguous .h files to C++ when either nearby files or
// the header itself make that likely. Plain C headers still stay on the C path.
func routeHeaderLang(repo, rel string, data []byte) string {
	if hasCPPSibling(repo, rel) || headerLooksCPP(data) {
		return "cpp"
	}
	return "c"
}

func hasCPPSibling(repo, rel string) bool {
	stem := strings.TrimSuffix(rel, filepath.Ext(rel))
	for _, ext := range []string{".cc", ".cpp", ".cxx", ".hpp", ".hh", ".hxx"} {
		if fi, err := os.Stat(filepath.Join(repo, stem+ext)); err == nil && !fi.IsDir() {
			return true
		}
	}
	return false
}

func headerLooksCPP(data []byte) bool {
	head := data
	if len(head) > 16_000 {
		head = head[:16_000]
	}
	s := string(head)
	for _, marker := range []string{
		"namespace ",
		"class ",
		"template <",
		"template<",
		"std::",
		"public:",
		"private:",
		"protected:",
		"#include <vector>",
		"#include <string>",
		"#include <memory>",
	} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}
