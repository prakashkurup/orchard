// Package graph builds a per-repository code graph — symbols (functions, types,
// methods, …) and the call/reference edges between them — and stores it in
// SQLite so an AI coding agent can query structure instead of reading whole
// files. See notes/codegraph-design.md for the full design.
//
// Parsing is pluggable: each language is handled by a ParserProvider. Go uses
// the standard library (precise); other languages use tree-sitter or ast-grep
// backends registered alongside it. All backends are pure-Go or out-of-process,
// so orchard stays a CGo-free binary.
package graph

import "context"

// Kind classifies a defined symbol.
type Kind string

const (
	KindFunc      Kind = "function"
	KindMethod    Kind = "method"
	KindClass     Kind = "class"
	KindStruct    Kind = "struct"
	KindInterface Kind = "interface"
	KindType      Kind = "type"
	KindVar       Kind = "var"
	KindConst     Kind = "const"
	KindModule    Kind = "module"
)

// EdgeKind classifies a relationship between a symbol and a referenced name.
type EdgeKind string

const (
	EdgeCall      EdgeKind = "call"
	EdgeReference EdgeKind = "reference"
	EdgeImport    EdgeKind = "import"
	EdgeInherit   EdgeKind = "inherit"
)

// Confidence labels how an edge's target was resolved. go/ast edges are
// syntactically exact; tree-sitter / ast-grep edges are heuristic name matches.
type Confidence string

const (
	// Extracted: the referenced name resolves to a single indexed definition.
	Extracted Confidence = "extracted"
	// Inferred: a reference whose target was not found (external / unknown).
	Inferred Confidence = "inferred"
	// Ambiguous: the name matches more than one indexed definition.
	Ambiguous Confidence = "ambiguous"
)

// Tier is the parse quality a provider expects for a language. It is surfaced to
// the agent so it knows how much to trust the graph versus reading the file.
type Tier string

const (
	TierPrecise     Tier = "precise"     // exact semantics (Go via go/ast)
	TierGood        Tier = "good"        // clean tree-sitter parse
	TierBestEffort  Tier = "best-effort" // partial parse; some symbols may be missing
	TierUnsupported Tier = "unsupported"
)

// Symbol is a defined entity in a file — a node in the graph.
type Symbol struct {
	Name      string
	Kind      Kind
	Signature string // one-line skeleton (no body), for the repo map
	StartLine int
	EndLine   int
}

// Edge is a reference from an enclosing symbol to a (possibly external) name.
type Edge struct {
	SrcName string // enclosing definition where the reference appears ("" = file scope)
	DstName string // the referenced name (the callee)
	Kind    EdgeKind
	Line    int
}

// FileResult is what a ParserProvider returns for one file.
type FileResult struct {
	Symbols     []Symbol
	Edges       []Edge
	Diagnostics []string // parse warnings/errors; empty means a clean parse
}

// SourceFile is one file handed to a provider: its repo-relative path and bytes.
type SourceFile struct {
	Rel  string
	Data []byte
}

// ParserProvider extracts symbols and edges from a batch of same-language files.
//
// Implementations are language backends (go/ast in-process; ast-grep
// out-of-process). Batch (rather than per-file) so an external tool like
// ast-grep can scan a whole repo in one invocation instead of spawning a
// process per file. A provider may handle several languages; Tier reports its
// expected quality per language so callers can label results.
type ParserProvider interface {
	// Name identifies the backend, e.g. "go/ast" or "ast-grep".
	Name() string
	// Tier reports the expected parse quality for a language label.
	Tier(lang string) Tier
	// Extract parses files (all of language lang, located under repoRoot) and
	// returns a FileResult keyed by SourceFile.Rel. A file that partially fails
	// should yield a FileResult with Diagnostics rather than aborting the batch,
	// so one bad file never fails the build.
	Extract(ctx context.Context, repoRoot, lang string, files []SourceFile) (map[string]FileResult, error)
}
