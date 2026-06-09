package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// astGrepProvider extracts symbols and edges by shelling out to ast-grep
// (https://ast-grep.github.io) — a Rust binary that bundles the real tree-sitter
// grammars (with their external scanners), so it parses Kotlin/C#/C/C++ etc.
// cleanly where the pure-Go grammars fall down. It is the "hard language"
// parser backend.
//
// It scans an explicit file set once per node kind (ast-grep is fast and
// parallel) rather than spawning a process per file — and because the set is
// explicit, incremental reindex can re-scan only the changed files.
type astGrepProvider struct{ bin string }

const astGrepPathEnv = "ORCHARD_AST_GREP_PATH"

// newASTGrep resolves the ast-grep binary: an explicit ORCHARD_AST_GREP_PATH
// override first, then orchard's managed copy under <user-config>/orchard/bin,
// then PATH as a convenience fallback. ok is false when it is not found, in
// which case the registry leaves those languages unsupported.
func newASTGrep() (astGrepProvider, bool) {
	names := []string{"ast-grep", "sg"}
	if p := os.Getenv(astGrepPathEnv); p != "" {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return astGrepProvider{bin: p}, true
		}
		return astGrepProvider{}, false
	}
	if dir, err := binDir(); err == nil {
		for _, name := range names {
			p := filepath.Join(dir, name)
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				return astGrepProvider{bin: p}, true
			}
		}
	}
	for _, name := range names {
		if p, err := exec.LookPath(name); err == nil {
			return astGrepProvider{bin: p}, true
		}
	}
	return astGrepProvider{}, false
}

func (astGrepProvider) Name() string { return "ast-grep" }

func (astGrepProvider) Tier(lang string) Tier {
	switch lang {
	case "python", "ruby", "java", "typescript", "javascript", "c":
		return TierGood
	case "kotlin", "csharp", "cpp", "tsx":
		return TierBestEffort
	}
	return TierUnsupported
}

// agDef describes one definition node kind for a language and how to read its
// name: keyword != "" means "the identifier after this keyword" (e.g. "fun",
// "class"); keyword == "" means "the identifier immediately before '('" (used
// for C-style method/function declarators that have no leading name keyword).
type agDef struct {
	kind    string
	symKind Kind
	keyword string
}

type agSpec struct {
	defs  []agDef
	calls []string // ast-grep node kinds that denote a call site
}

// agByLang maps a language label to the ast-grep node kinds we extract. The kind
// names are tree-sitter grammar node kinds, verified against real repos.
var agByLang = map[string]agSpec{
	"python": {
		defs:  []agDef{{"function_definition", KindFunc, "def"}, {"class_definition", KindClass, "class"}},
		calls: []string{"call"},
	},
	"ruby": {
		defs:  []agDef{{"method", KindMethod, "def"}, {"class", KindClass, "class"}, {"module", KindModule, "module"}},
		calls: []string{"call"},
	},
	"kotlin": {
		defs:  []agDef{{"function_declaration", KindFunc, "fun"}, {"class_declaration", KindClass, "class"}, {"object_declaration", KindClass, "object"}},
		calls: []string{"call_expression"},
	},
	"java": {
		defs:  []agDef{{"method_declaration", KindMethod, ""}, {"class_declaration", KindClass, "class"}, {"interface_declaration", KindInterface, "interface"}},
		calls: []string{"method_invocation"},
	},
	"csharp": {
		defs:  []agDef{{"method_declaration", KindMethod, ""}, {"class_declaration", KindClass, "class"}, {"interface_declaration", KindInterface, "interface"}, {"struct_declaration", KindStruct, "struct"}},
		calls: []string{"invocation_expression"},
	},
	"typescript": {
		defs:  []agDef{{"function_declaration", KindFunc, "function"}, {"method_definition", KindMethod, ""}, {"class_declaration", KindClass, "class"}},
		calls: []string{"call_expression"},
	},
	"javascript": {
		defs:  []agDef{{"function_declaration", KindFunc, "function"}, {"method_definition", KindMethod, ""}, {"class_declaration", KindClass, "class"}},
		calls: []string{"call_expression"},
	},
	"c": {
		defs:  []agDef{{"function_definition", KindFunc, ""}},
		calls: []string{"call_expression"},
	},
	"cpp": {
		defs:  []agDef{{"function_definition", KindFunc, ""}, {"class_specifier", KindClass, "class"}, {"struct_specifier", KindStruct, "struct"}},
		calls: []string{"call_expression"},
	},
}

func init() { agByLang["tsx"] = agByLang["typescript"] }

// agMatch is the subset of ast-grep's --json output we use.
type agMatch struct {
	Text  string `json:"text"`
	File  string `json:"file"`
	Range struct {
		Start struct {
			Line int `json:"line"`
		} `json:"start"`
		End struct {
			Line int `json:"line"`
		} `json:"end"`
	} `json:"range"`
}

// agScanChunk bounds how many file paths are passed to one ast-grep invocation
// (keeps the argv well under ARG_MAX on large repos).
const agScanChunk = 300

// scan runs ast-grep for a single node kind over the given repo-relative paths
// (chunked) and returns matches. Scanning an explicit file set — rather than the
// whole repo — is what lets incremental reindex re-scan only changed files. It
// tolerates a non-zero exit and parses whatever JSON came back on stdout.
func (a astGrepProvider) scan(ctx context.Context, repoRoot, lang, kind string, rels []string) ([]agMatch, error) {
	rule := fmt.Sprintf("id: r\nlanguage: %s\nrule:\n  kind: %s\n", lang, kind)
	var matches []agMatch
	for start := 0; start < len(rels); start += agScanChunk {
		end := start + agScanChunk
		if end > len(rels) {
			end = len(rels)
		}
		args := append([]string{"scan", "--inline-rules", rule, "--json=stream"}, rels[start:end]...)
		cmd := exec.CommandContext(ctx, a.bin, args...)
		cmd.Dir = repoRoot
		out, err := cmd.Output()
		// A non-zero exit with output is expected (ast-grep signals matches that
		// way); we parse stdout regardless. But if the binary could not run at all
		// (missing, not executable, killed), surface that instead of silently
		// returning an empty graph.
		var exitErr *exec.ExitError
		if err != nil && !errors.As(err, &exitErr) {
			return nil, fmt.Errorf("ast-grep scan (%s/%s): %w", lang, kind, err)
		}
		dec := json.NewDecoder(bytes.NewReader(out))
		for {
			var m agMatch
			if err := dec.Decode(&m); err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				break
			}
			matches = append(matches, m)
		}
	}
	return matches, nil
}

// Extract runs ast-grep once per def/call kind over the repo, keeps matches in
// the allowed file set, reads names heuristically from node text, and resolves
// each call's enclosing definition by innermost line-range containment.
func (a astGrepProvider) Extract(ctx context.Context, repoRoot, lang string, files []SourceFile) (map[string]FileResult, error) {
	spec, ok := agByLang[lang]
	if !ok {
		return nil, nil
	}
	allowed := make(map[string]bool, len(files))
	rels := make([]string, len(files))
	for i, f := range files {
		allowed[f.Rel] = true
		rels[i] = f.Rel
	}

	results := map[string]*FileResult{}
	get := func(rel string) *FileResult {
		if r := results[rel]; r != nil {
			return r
		}
		r := &FileResult{}
		results[rel] = r
		return r
	}

	type span struct {
		start, end int
		name       string
	}
	spans := map[string][]span{}

	for _, d := range spec.defs {
		ms, err := a.scan(ctx, repoRoot, lang, d.kind, rels)
		if err != nil {
			return nil, err
		}
		for _, m := range ms {
			rel := normalizeRel(m.File)
			if !allowed[rel] {
				continue
			}
			name := defName(d, m.Text)
			if name == "" {
				continue
			}
			sl, el := m.Range.Start.Line+1, m.Range.End.Line+1
			get(rel).Symbols = append(get(rel).Symbols, Symbol{
				Name: name, Kind: d.symKind, Signature: string(d.symKind) + " " + name,
				StartLine: sl, EndLine: el,
			})
			spans[rel] = append(spans[rel], span{sl, el, name})
		}
	}

	enclosing := func(rel string, line int) string {
		best, bestSize := "", int(^uint(0)>>1)
		for _, sp := range spans[rel] {
			if line >= sp.start && line <= sp.end {
				if sz := sp.end - sp.start; sz < bestSize {
					bestSize, best = sz, sp.name
				}
			}
		}
		return best
	}

	for _, ck := range spec.calls {
		ms, err := a.scan(ctx, repoRoot, lang, ck, rels)
		if err != nil {
			return nil, err
		}
		for _, m := range ms {
			rel := normalizeRel(m.File)
			if !allowed[rel] {
				continue
			}
			callee := identBeforeParen(m.Text)
			if callee == "" {
				continue
			}
			line := m.Range.Start.Line + 1
			get(rel).Edges = append(get(rel).Edges, Edge{
				SrcName: enclosing(rel, line), DstName: callee, Kind: EdgeCall, Line: line,
			})
		}
	}

	out := make(map[string]FileResult, len(results))
	for rel, r := range results {
		out[rel] = *r
	}
	return out, nil
}

func normalizeRel(p string) string { return strings.TrimPrefix(p, "./") }

var reIdentBeforeParen = regexp.MustCompile(`([A-Za-z_]\w*)\s*\(`)

// identBeforeParen returns the identifier immediately before the first '(' —
// the callee of a call expression ("a.b(x)" → "b", "foo(y)" → "foo").
func identBeforeParen(s string) string {
	if m := reIdentBeforeParen.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}

var (
	kwReMu sync.Mutex
	kwRe   = map[string]*regexp.Regexp{}
)

// defName reads a definition's name from its node text: the identifier after the
// keyword (e.g. "fun foo" → "foo"), or — for keyword-less C-style declarators —
// the identifier before '('. The keyword-regex cache is mutex-guarded because
// Extract runs concurrently across repos/languages.
func defName(d agDef, text string) string {
	if d.keyword == "" {
		return identBeforeParen(text)
	}
	kwReMu.Lock()
	re := kwRe[d.keyword]
	if re == nil {
		re = regexp.MustCompile(`\b` + regexp.QuoteMeta(d.keyword) + `\s+([A-Za-z_]\w*)`)
		kwRe[d.keyword] = re
	}
	kwReMu.Unlock()
	if m := re.FindStringSubmatch(text); m != nil {
		return m[1]
	}
	return ""
}
