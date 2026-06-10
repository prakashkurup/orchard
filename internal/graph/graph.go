package graph

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Graph is a per-repository code graph backed by SQLite.
type Graph struct{ store *Store }

// Open opens (or creates) the graph database at dbPath.
func Open(dbPath string) (*Graph, error) {
	s, err := openStore(dbPath)
	if err != nil {
		return nil, err
	}
	return &Graph{store: s}, nil
}

// Close closes the underlying database.
func (g *Graph) Close() error { return g.store.Close() }

// fileMeta is the per-file metadata stored in the files table.
type fileMeta struct {
	Path, Lang, SHA string
	Bytes           int
	Tier            Tier
}

// FileGraph is one file's contribution to the graph: its metadata plus the
// symbols and edges a provider extracted from it.
type FileGraph struct {
	File    fileMeta
	Symbols []Symbol
	Edges   []Edge
}

// BuildStats summarizes a build for reporting.
type BuildStats struct {
	Files         int
	Symbols       int
	Edges         int
	ResolvedEdges int
	Skipped       int          // dropped by binary/generated/oversize filters
	Unsupported   int          // recognized language with no registered provider
	Diagnostics   int          // files that parsed with warnings/errors
	ByTier        map[Tier]int // files per quality tier
}

// LangTrust describes the parser quality used for a language in a built graph.
type LangTrust struct {
	Lang  string `json:"language"`
	Tier  Tier   `json:"tier"`
	Files int    `json:"files"`
}

// extract groups files by language and runs each provider as one batch,
// returning the per-file graphs plus tallies. Shared by Build and Update.
func extract(ctx context.Context, repoRoot string, reg *Registry, dfs []DiscoveredFile) (fgs []FileGraph, byTier map[Tier]int, unsupported, diagnostics int) {
	byTier = map[Tier]int{}
	byLang := map[string][]DiscoveredFile{}
	order := make([]string, 0)
	for _, df := range dfs {
		if _, seen := byLang[df.Lang]; !seen {
			order = append(order, df.Lang)
		}
		byLang[df.Lang] = append(byLang[df.Lang], df)
	}
	for _, lang := range order {
		group := byLang[lang]
		p, ok := reg.For(lang)
		if !ok {
			unsupported += len(group)
			continue
		}
		srcs := make([]SourceFile, len(group))
		for i, df := range group {
			srcs[i] = SourceFile{Rel: df.Rel, Data: df.Data}
		}
		out, err := p.Extract(ctx, repoRoot, lang, srcs)
		if err != nil {
			diagnostics += len(group)
			continue
		}
		tier := p.Tier(lang)
		for _, df := range group {
			res := out[df.Rel]
			if len(res.Diagnostics) > 0 {
				diagnostics++
			}
			byTier[tier]++
			fgs = append(fgs, FileGraph{
				File:    fileMeta{Path: df.Rel, Lang: lang, SHA: df.SHA, Bytes: len(df.Data), Tier: tier},
				Symbols: res.Symbols,
				Edges:   res.Edges,
			})
		}
	}
	return fgs, byTier, unsupported, diagnostics
}

// Build performs a full (re)index of repoPath, choosing a provider per language
// via reg, and replaces the graph. It is I/O- and CPU-heavy; run it off the UI
// thread.
func (g *Graph) Build(ctx context.Context, repoPath string, reg *Registry) (BuildStats, error) {
	discovered, skipped, err := Discover(ctx, repoPath)
	if err != nil {
		return BuildStats{}, err
	}
	fgs, byTier, unsupported, diagnostics := extract(ctx, repoPath, reg, discovered)
	if err := g.store.replace(fgs); err != nil {
		return BuildStats{}, err
	}
	if err := g.store.rankPass(); err != nil {
		return BuildStats{}, err
	}
	g.recordFreshness(repoPath)
	stats := BuildStats{Skipped: skipped, Unsupported: unsupported, Diagnostics: diagnostics, ByTier: byTier}
	stats.Files, stats.Symbols, stats.Edges, stats.ResolvedEdges = g.store.counts()
	return stats, nil
}

// UpdateStats summarizes an incremental update.
type UpdateStats struct {
	Changed, Added, Deleted, Reused      int
	Files, Symbols, Edges, ResolvedEdges int
}

// Update incrementally reindexes repoPath: it re-parses only files whose content
// hash changed (or are new), reuses unchanged files from the DB, and re-resolves
// the whole graph — producing the same result as a full Build but without
// re-parsing untouched files. If nothing changed it only refreshes metadata.
func (g *Graph) Update(ctx context.Context, repoPath string, reg *Registry) (UpdateStats, error) {
	discovered, _, err := Discover(ctx, repoPath)
	if err != nil {
		return UpdateStats{}, err
	}
	stored, err := g.store.snapshot()
	if err != nil {
		return UpdateStats{}, err
	}

	var us UpdateStats
	cur := make(map[string]bool, len(discovered))
	keep := map[string]fileMeta{}
	var dirty []DiscoveredFile
	for _, df := range discovered {
		cur[df.Rel] = true
		if old, ok := stored[df.Rel]; ok {
			if old.SHA == df.SHA {
				keep[df.Rel] = old
				us.Reused++
				continue
			}
			us.Changed++
		} else {
			us.Added++
		}
		dirty = append(dirty, df)
	}
	for rel := range stored {
		if !cur[rel] {
			us.Deleted++
		}
	}

	if us.Changed == 0 && us.Added == 0 && us.Deleted == 0 {
		g.recordFreshness(repoPath) // still refresh built-at / dirty count
		us.Files, us.Symbols, us.Edges, us.ResolvedEdges = g.store.counts()
		return us, nil
	}

	fresh, _, _, _ := extract(ctx, repoPath, reg, dirty)
	reused, err := g.store.reconstruct(keep)
	if err != nil {
		return us, err
	}
	if err := g.store.replace(append(reused, fresh...)); err != nil {
		return us, err
	}
	if err := g.store.rankPass(); err != nil {
		return us, err
	}
	g.recordFreshness(repoPath)
	us.Files, us.Symbols, us.Edges, us.ResolvedEdges = g.store.counts()
	return us, nil
}

// Stale reports whether the working tree differs from the indexed graph (by
// content hash) without rebuilding, and how many files changed/added/deleted.
func (g *Graph) Stale(ctx context.Context, repoPath string) (stale bool, changed int, err error) {
	discovered, _, err := Discover(ctx, repoPath)
	if err != nil {
		return false, 0, err
	}
	stored, err := g.store.snapshot()
	if err != nil {
		return false, 0, err
	}
	cur := make(map[string]string, len(discovered))
	for _, df := range discovered {
		cur[df.Rel] = df.SHA
		if old, ok := stored[df.Rel]; !ok || old.SHA != df.SHA {
			changed++
		}
	}
	for rel := range stored {
		if _, ok := cur[rel]; !ok {
			changed++
		}
	}
	return changed > 0, changed, nil
}

// Freshness describes when/what the graph was last built against.
type Freshness struct {
	BuiltAt    time.Time
	HeadCommit string
	DirtyFiles int
}

// Freshness returns the recorded build metadata (for surfacing to the agent).
func (g *Graph) Freshness() Freshness {
	f := Freshness{HeadCommit: g.store.getMeta("head_commit")}
	if u, err := strconv.ParseInt(g.store.getMeta("built_at_unix"), 10, 64); err == nil && u > 0 {
		f.BuiltAt = time.Unix(u, 0)
	}
	if d, err := strconv.Atoi(g.store.getMeta("dirty_files")); err == nil {
		f.DirtyFiles = d
	}
	return f
}

func (g *Graph) recordFreshness(repoPath string) {
	commit, dirty := gitInfo(repoPath)
	g.store.setMeta(map[string]string{
		"built_at_unix": strconv.FormatInt(time.Now().Unix(), 10),
		"head_commit":   commit,
		"dirty_files":   strconv.Itoa(dirty),
	})
}

// gitInfo returns the repo's HEAD commit and dirty-file count (best-effort).
func gitInfo(repoPath string) (commit string, dirty int) {
	if out, err := exec.Command("git", "-C", repoPath, "rev-parse", "HEAD").Output(); err == nil {
		commit = strings.TrimSpace(string(out))
	}
	if out, err := exec.Command("git", "-C", repoPath, "status", "--porcelain").Output(); err == nil {
		for _, l := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
			if strings.TrimSpace(l) != "" {
				dirty++
			}
		}
	}
	return commit, dirty
}

// Counts returns the graph's row totals plus how many edges resolved.
func (g *Graph) Counts() (files, symbols, edges, resolvedEdges int) { return g.store.counts() }

// TierCounts returns indexed file counts grouped by parser quality tier.
func (g *Graph) TierCounts() map[Tier]int {
	tiers, _ := g.store.tierCounts()
	return tiers
}

// TrustLabels returns indexed file counts grouped by language and parser tier.
func (g *Graph) TrustLabels() []LangTrust {
	trust, _ := g.store.trustLabels()
	return trust
}

// FindDef returns up to limit definition sites for a symbol name, important first.
func (g *Graph) FindDef(name string, limit int) ([]DefRow, error) {
	return g.store.findDef(name, limit)
}

// WhoCalls returns inbound call/reference sites for a symbol name, ordered by
// caller importance, paginated by limit/offset.
func (g *Graph) WhoCalls(name string, limit, offset int) ([]CallerRow, error) {
	return g.store.whoCalls(name, limit, offset)
}

// BlastRadius returns everything transitively reachable backwards from name (its
// callers, their callers, …) up to maxDepth, capped at limit — the impact of
// changing it.
func (g *Graph) BlastRadius(name string, maxDepth, limit int) ([]ImpactRow, error) {
	return g.store.blastRadius(name, maxDepth, limit)
}

// RepoMap returns the top-ranked definitions (a PageRank-ordered skeleton),
// capped at limit — the token-budgeted overview of the repo.
func (g *Graph) RepoMap(limit int) ([]MapRow, error) { return g.store.repoMap(limit) }

// SearchSymbols finds definitions whose name contains query, important first,
// capped at limit.
func (g *Graph) SearchSymbols(query string, limit int) ([]DefRow, error) {
	return g.store.searchSymbols(query, limit)
}
