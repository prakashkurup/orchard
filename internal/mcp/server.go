// Package mcp serves one or more repository code graphs to AI coding agents
// (Claude Code, OpenAI Codex) over the Model Context Protocol, so the agent
// queries structure instead of reading whole files. With several repos it
// answers across all of them (orchard's cross-repo view). It is read-only and
// exposes signatures, never file bodies. See notes/codegraph-design.md §8.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prakashkurup/orchard/internal/graph"
)

// caps keep tool output well under an agent's per-response token budget.
const (
	defaultLimit  = 50
	maxLimit      = 500
	repoMapLimit  = 100
	blastMaxDepth = 6
)

// RepoGraph pairs a repo's display name with its open graph.
type RepoGraph struct {
	Name  string
	Path  string
	G     *graph.Graph
	State *RepoState
}

// RepoState carries live indexing status for an MCP-served repo. It is separate
// from RepoGraph so RepoGraph values can be copied safely while sharing status.
type RepoState struct {
	Indexing  atomic.Bool
	LastError atomic.Value // string
}

// NewRepoGraph creates a RepoGraph with live status tracking enabled.
func NewRepoGraph(name string, g *graph.Graph, path ...string) RepoGraph {
	r := RepoGraph{Name: name, G: g, State: &RepoState{}}
	if len(path) > 0 {
		r.Path = path[0]
	}
	return r
}

// SetIndexing updates whether this repo is currently being refreshed.
func (r RepoGraph) SetIndexing(v bool) {
	if r.State != nil {
		r.State.Indexing.Store(v)
	}
}

// Indexing reports whether this repo is currently being refreshed.
func (r RepoGraph) Indexing() bool {
	return r.State != nil && r.State.Indexing.Load()
}

// SetError records the last background indexing error, if any.
func (r RepoGraph) SetError(err error) {
	if r.State == nil {
		return
	}
	if err == nil {
		r.State.LastError.Store("")
		return
	}
	r.State.LastError.Store(err.Error())
}

// Error returns the last background indexing error string.
func (r RepoGraph) Error() string {
	if r.State == nil {
		return ""
	}
	v, _ := r.State.LastError.Load().(string)
	return v
}

// Serve runs the MCP server for the given repos over stdio until ctx is cancelled.
func Serve(ctx context.Context, repos []RepoGraph) error {
	return newServer(repos).Run(ctx, &sdk.StdioTransport{})
}

func newServer(repos []RepoGraph) *sdk.Server {
	s := sdk.NewServer(&sdk.Implementation{Name: "orchard-codegraph", Version: "0.1.0"}, nil)
	addTools(s, repos)
	return s
}

func commitShort(f graph.Freshness) string {
	c := f.HeadCommit
	if len(c) > 8 {
		c = c[:8]
	}
	if c == "" {
		c = "?"
	}
	return c
}

func builtStr(f graph.Freshness) string {
	if f.BuiltAt.IsZero() {
		return "never"
	}
	return f.BuiltAt.Format(time.RFC3339)
}

// freshAll summarises freshness across all served repos.
func freshAll(repos []RepoGraph) string {
	parts := make([]string, 0, len(repos))
	for _, r := range repos {
		f := r.G.Freshness()
		part := fmt.Sprintf("%s@%s(%dd)", r.Name, commitShort(f), f.DirtyFiles)
		if r.Indexing() {
			part += ",indexing"
		}
		if err := r.Error(); err != "" {
			part += ",error=" + truncateStatus(err)
		}
		parts = append(parts, part)
	}
	return "graph: " + strings.Join(parts, " · ")
}

func truncateStatus(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 80 {
		return s
	}
	return s[:79] + "…"
}

func reply[T any](out T) (*sdk.CallToolResult, T, error) {
	b, _ := json.MarshalIndent(out, "", "  ")
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(b)}}}, out, nil
}

func limitOr(v, def int) int {
	if v <= 0 {
		return def
	}
	if v > maxLimit {
		return maxLimit
	}
	return v
}

// ---- tool I/O types (each result row is tagged with its repo) ----

type mapHit struct {
	Repo string `json:"repo"`
	graph.MapRow
}
type defHit struct {
	Repo string `json:"repo"`
	graph.DefRow
}
type callerHit struct {
	Repo string `json:"repo"`
	graph.CallerRow
}
type impactHit struct {
	Repo string `json:"repo"`
	graph.ImpactRow
}

type repoMapIn struct {
	Limit int `json:"limit,omitempty" jsonschema:"max entries to return (default 100)"`
}
type repoMapOut struct {
	Freshness string      `json:"freshness"`
	Trust     []repoTrust `json:"trust,omitempty"`
	Entries   []mapHit    `json:"entries"`
}

type nameIn struct {
	Name  string `json:"name" jsonschema:"the symbol name"`
	Limit int    `json:"limit,omitempty"`
}
type searchIn struct {
	Query string `json:"query" jsonschema:"substring to match against symbol names"`
	Limit int    `json:"limit,omitempty"`
}
type defOut struct {
	Freshness string      `json:"freshness"`
	Trust     []repoTrust `json:"trust,omitempty"`
	Defs      []defHit    `json:"defs"`
}

type whoCallsIn struct {
	Name   string `json:"name" jsonschema:"the symbol whose callers you want"`
	Limit  int    `json:"limit,omitempty"`
	Offset int    `json:"offset,omitempty"`
}
type callersOut struct {
	Freshness string      `json:"freshness"`
	Trust     []repoTrust `json:"trust,omitempty"`
	Callers   []callerHit `json:"callers"`
}

type blastIn struct {
	Name     string `json:"name" jsonschema:"the symbol you intend to change"`
	MaxDepth int    `json:"max_depth,omitempty" jsonschema:"max traversal depth (default 6)"`
	Limit    int    `json:"limit,omitempty"`
}
type blastOut struct {
	Freshness string      `json:"freshness"`
	Trust     []repoTrust `json:"trust,omitempty"`
	Impacted  []impactHit `json:"impacted"`
}

type statusIn struct{}
type trustLabel struct {
	Language string `json:"language"`
	Tier     string `json:"tier"`
	Files    int    `json:"files"`
}
type repoTrust struct {
	Repo      string       `json:"repo"`
	Languages []trustLabel `json:"languages"`
}
type repoStatus struct {
	Repo          string         `json:"repo"`
	Freshness     string         `json:"freshness"`
	Stale         bool           `json:"stale"`
	StaleReasons  []string       `json:"stale_reasons,omitempty"`
	ChangedFiles  int            `json:"changed_files,omitempty"`
	Indexing      bool           `json:"indexing"`
	LastError     string         `json:"last_error,omitempty"`
	Coverage      string         `json:"coverage"`
	Files         int            `json:"files"`
	Symbols       int            `json:"symbols"`
	Edges         int            `json:"edges"`
	ResolvedEdges int            `json:"resolved_edges"`
	Tiers         map[string]int `json:"tiers,omitempty"`
	Trust         []trustLabel   `json:"trust,omitempty"`
}
type statusOut struct {
	Repos []repoStatus `json:"repos"`
}

func sortDefHits(hits []defHit) {
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Rank != hits[j].Rank {
			return hits[i].Rank > hits[j].Rank
		}
		if hits[i].Repo != hits[j].Repo {
			return hits[i].Repo < hits[j].Repo
		}
		if hits[i].Path != hits[j].Path {
			return hits[i].Path < hits[j].Path
		}
		return hits[i].Line < hits[j].Line
	})
}

func sortCallerHits(hits []callerHit) {
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Rank != hits[j].Rank {
			return hits[i].Rank > hits[j].Rank
		}
		if hits[i].Repo != hits[j].Repo {
			return hits[i].Repo < hits[j].Repo
		}
		if hits[i].Path != hits[j].Path {
			return hits[i].Path < hits[j].Path
		}
		return hits[i].Line < hits[j].Line
	})
}

func sortImpactHits(hits []impactHit) {
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Depth != hits[j].Depth {
			return hits[i].Depth < hits[j].Depth
		}
		if hits[i].Repo != hits[j].Repo {
			return hits[i].Repo < hits[j].Repo
		}
		if hits[i].Path != hits[j].Path {
			return hits[i].Path < hits[j].Path
		}
		return hits[i].Name < hits[j].Name
	})
}

func tierCountsForJSON(tiers map[graph.Tier]int) map[string]int {
	if len(tiers) == 0 {
		return nil
	}
	out := make(map[string]int, len(tiers))
	for tier, count := range tiers {
		out[string(tier)] = count
	}
	return out
}

func trustLabelsForJSON(trust []graph.LangTrust) []trustLabel {
	if len(trust) == 0 {
		return nil
	}
	out := make([]trustLabel, 0, len(trust))
	for _, t := range trust {
		out = append(out, trustLabel{
			Language: displayLang(t.Lang),
			Tier:     string(t.Tier),
			Files:    t.Files,
		})
	}
	return out
}

func trustAll(repos []RepoGraph) []repoTrust {
	out := make([]repoTrust, 0, len(repos))
	for _, r := range repos {
		labels := trustLabelsForJSON(r.G.TrustLabels())
		if len(labels) == 0 {
			continue
		}
		out = append(out, repoTrust{Repo: r.Name, Languages: labels})
	}
	return out
}

func displayLang(lang string) string {
	switch lang {
	case "go":
		return "Go"
	case "python":
		return "Python"
	case "ruby":
		return "Ruby"
	case "java":
		return "Java"
	case "kotlin":
		return "Kotlin"
	case "csharp":
		return "C#"
	case "typescript", "tsx":
		return "TypeScript"
	case "javascript":
		return "JavaScript"
	case "c":
		return "C"
	case "cpp":
		return "C++"
	default:
		return lang
	}
}

func staleForRepo(ctx context.Context, r RepoGraph, f graph.Freshness) (bool, int, []string) {
	var changed int
	var stale bool
	if r.Path != "" {
		if s, c, err := r.G.Stale(ctx, r.Path); err == nil {
			stale, changed = s, c
		}
	}
	var reasons []string
	if changed > 0 {
		reasons = append(reasons, fmt.Sprintf("%d file%s changed", changed, plural(changed)))
	}
	if f.DirtyFiles > 0 {
		reasons = append(reasons, "built from dirty tree")
	}
	if r.Indexing() {
		reasons = append(reasons, "indexing")
	}
	if len(reasons) > 0 {
		stale = true
	}
	return stale, changed, reasons
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func addTools(s *sdk.Server, repos []RepoGraph) {
	sdk.AddTool(s, &sdk.Tool{
		Name:        "repo_map",
		Description: "Ranked skeleton of the repository/repositories (most-important symbols first, by PageRank). Read this for a fast overview instead of listing or reading files.",
	}, func(_ context.Context, _ *sdk.CallToolRequest, in repoMapIn) (*sdk.CallToolResult, repoMapOut, error) {
		lim := limitOr(in.Limit, repoMapLimit)
		var hits []mapHit
		for _, r := range repos {
			rows, err := r.G.RepoMap(lim)
			if err != nil {
				return nil, repoMapOut{}, fmt.Errorf("%s: %w", r.Name, err)
			}
			for _, m := range rows {
				hits = append(hits, mapHit{Repo: r.Name, MapRow: m})
			}
		}
		sort.SliceStable(hits, func(i, j int) bool { return hits[i].Rank > hits[j].Rank })
		if len(hits) > lim {
			hits = hits[:lim]
		}
		return reply(repoMapOut{Freshness: freshAll(repos), Trust: trustAll(repos), Entries: hits})
	})

	sdk.AddTool(s, &sdk.Tool{
		Name:        "find_definition",
		Description: "Find where a symbol is defined (file, line, signature) across the served repos, without grepping.",
	}, func(_ context.Context, _ *sdk.CallToolRequest, in nameIn) (*sdk.CallToolResult, defOut, error) {
		lim := limitOr(in.Limit, defaultLimit)
		var hits []defHit
		for _, r := range repos {
			rows, err := r.G.FindDef(in.Name, lim)
			if err != nil {
				return nil, defOut{}, fmt.Errorf("%s: %w", r.Name, err)
			}
			for _, d := range rows {
				hits = append(hits, defHit{Repo: r.Name, DefRow: d})
			}
		}
		sortDefHits(hits)
		if len(hits) > lim {
			hits = hits[:lim]
		}
		return reply(defOut{Freshness: freshAll(repos), Trust: trustAll(repos), Defs: hits})
	})

	sdk.AddTool(s, &sdk.Tool{
		Name:        "who_calls",
		Description: "List the call/reference sites of a symbol (its callers) across the served repos — orchard's cross-repo usage search. Use instead of grepping.",
	}, func(_ context.Context, _ *sdk.CallToolRequest, in whoCallsIn) (*sdk.CallToolResult, callersOut, error) {
		lim := limitOr(in.Limit, defaultLimit)
		offset := in.Offset
		if offset < 0 {
			offset = 0
		}
		if offset > maxLimit { // bound so lim+offset cannot overflow into a negative LIMIT
			offset = maxLimit
		}
		var hits []callerHit
		for _, r := range repos {
			rows, err := r.G.WhoCalls(in.Name, lim+offset, 0)
			if err != nil {
				return nil, callersOut{}, fmt.Errorf("%s: %w", r.Name, err)
			}
			for _, c := range rows {
				hits = append(hits, callerHit{Repo: r.Name, CallerRow: c})
			}
		}
		sortCallerHits(hits)
		if offset > 0 {
			if offset < len(hits) {
				hits = hits[offset:]
			} else {
				hits = nil
			}
		}
		if len(hits) > lim {
			hits = hits[:lim]
		}
		return reply(callersOut{Freshness: freshAll(repos), Trust: trustAll(repos), Callers: hits})
	})

	sdk.AddTool(s, &sdk.Tool{
		Name:        "blast_radius",
		Description: "Everything transitively affected by changing a symbol (its callers, their callers, …) within each served repo. Use to scope the impact of an edit.",
	}, func(_ context.Context, _ *sdk.CallToolRequest, in blastIn) (*sdk.CallToolResult, blastOut, error) {
		depth := in.MaxDepth
		if depth <= 0 || depth > 20 {
			depth = blastMaxDepth
		}
		lim := limitOr(in.Limit, maxLimit)
		var hits []impactHit
		for _, r := range repos {
			rows, err := r.G.BlastRadius(in.Name, depth, lim)
			if err != nil {
				return nil, blastOut{}, fmt.Errorf("%s: %w", r.Name, err)
			}
			for _, x := range rows {
				hits = append(hits, impactHit{Repo: r.Name, ImpactRow: x})
			}
		}
		sortImpactHits(hits)
		if len(hits) > lim {
			hits = hits[:lim]
		}
		return reply(blastOut{Freshness: freshAll(repos), Trust: trustAll(repos), Impacted: hits})
	})

	sdk.AddTool(s, &sdk.Tool{
		Name:        "search_symbols",
		Description: "Find symbols whose name contains a substring across the served repos, most important first.",
	}, func(_ context.Context, _ *sdk.CallToolRequest, in searchIn) (*sdk.CallToolResult, defOut, error) {
		lim := limitOr(in.Limit, defaultLimit)
		var hits []defHit
		for _, r := range repos {
			rows, err := r.G.SearchSymbols(in.Query, lim)
			if err != nil {
				return nil, defOut{}, fmt.Errorf("%s: %w", r.Name, err)
			}
			for _, d := range rows {
				hits = append(hits, defHit{Repo: r.Name, DefRow: d})
			}
		}
		sortDefHits(hits)
		if len(hits) > lim {
			hits = hits[:lim]
		}
		return reply(defOut{Freshness: freshAll(repos), Trust: trustAll(repos), Defs: hits})
	})

	sdk.AddTool(s, &sdk.Tool{
		Name:        "status",
		Description: "Per-repo graph freshness (commit it was built at, dirty-file count) and size. Check this to know whether the graph is up to date.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, _ statusIn) (*sdk.CallToolResult, statusOut, error) {
		out := make([]repoStatus, 0, len(repos))
		for _, r := range repos {
			files, symbols, edges, resolved := r.G.Counts()
			f := r.G.Freshness()
			stale, changed, staleReasons := staleForRepo(ctx, r, f)
			out = append(out, repoStatus{
				Repo:          r.Name,
				Freshness:     fmt.Sprintf("commit %s · %d dirty · built %s", commitShort(f), f.DirtyFiles, builtStr(f)),
				Stale:         stale,
				StaleReasons:  staleReasons,
				ChangedFiles:  changed,
				Indexing:      r.Indexing(),
				LastError:     r.Error(),
				Coverage:      fmt.Sprintf("%d files · %d symbols · %d/%d edges resolved", files, symbols, resolved, edges),
				Files:         files,
				Symbols:       symbols,
				Edges:         edges,
				ResolvedEdges: resolved,
				Tiers:         tierCountsForJSON(r.G.TierCounts()),
				Trust:         trustLabelsForJSON(r.G.TrustLabels()),
			})
		}
		return reply(statusOut{Repos: out})
	})
}
