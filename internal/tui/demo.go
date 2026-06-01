package tui

import (
	"os"
	"strings"
	"time"

	"github.com/prakashkurup/orchard/internal/claude"
	orchardgit "github.com/prakashkurup/orchard/internal/git"
	"github.com/prakashkurup/orchard/internal/github"
	"github.com/prakashkurup/orchard/internal/lang"
	"github.com/prakashkurup/orchard/internal/repo"
	"github.com/prakashkurup/orchard/internal/search"
)

// demoMode reports whether the dashboard should render fictional data instead of
// scanning real repositories. Set ORCHARD_DEMO=1 to capture clean screenshots
// with no private repo names, branches, tickets, or source in them.
func demoMode() bool {
	v := os.Getenv("ORCHARD_DEMO")
	return v == "1" || strings.EqualFold(v, "true")
}

// demoSpec is a compact description of one fictional repo, expanded into a full
// repo.Repo by demoRepos. Everything here is invented.
type demoSpec struct {
	name     string
	branch   string
	ext      string // language extension, e.g. "go"
	rel      string // last-commit relative time
	subject  string // last-commit subject
	ahead    int
	behind   int
	changed  int
	stashes  int
	syncedH  int   // hours since last fetch
	activity []int // 12 weekly commit counts
	detached bool
	noUp     bool // no upstream configured
}

func demoSpecs() []demoSpec {
	return []demoSpec{
		{"acme-web", "feat/checkout-redesign", "ts", "2 hours ago", "Redesign the checkout summary card", 2, 0, 3, 0, 1, []int{1, 2, 0, 4, 6, 3, 7, 9, 5, 8, 6, 9}, false, false},
		{"payments-api", "main", "go", "yesterday", "Add idempotency keys to charge endpoint", 0, 0, 0, 0, 1, []int{4, 5, 3, 6, 4, 7, 5, 6, 8, 5, 7, 6}, false, false},
		{"design-system", "main", "ts", "3 days ago", "Bump tokens to v4, add focus rings", 0, 5, 0, 0, 2, []int{2, 1, 3, 0, 2, 1, 0, 3, 1, 2, 0, 1}, false, false},
		{"auth-service", "main", "go", "5 hours ago", "Rotate signing keys on a schedule", 1, 0, 0, 0, 1, []int{0, 0, 2, 3, 1, 4, 2, 3, 5, 2, 4, 3}, false, false},
		{"billing-worker", "fix/rate-limit", "py", "6 hours ago", "Back off retries under provider rate limits", 0, 0, 7, 0, 1, []int{1, 0, 2, 1, 3, 2, 4, 1, 2, 5, 3, 4}, false, false},
		{"notification-hub", "main", "go", "2 days ago", "Batch digest emails per user", 0, 0, 0, 1, 2, []int{0, 2, 1, 0, 3, 1, 2, 0, 1, 2, 1, 0}, false, false},
		{"data-pipeline", "main", "py", "4 days ago", "Partition events by day for replay", 1, 2, 0, 0, 3, []int{3, 4, 2, 5, 3, 6, 4, 2, 5, 3, 4, 2}, false, false},
		{"mobile-app", "main", "swift", "9 days ago", "Add dark mode and dynamic type", 0, 0, 0, 0, 26, []int{0, 0, 1, 0, 0, 2, 0, 1, 0, 0, 1, 0}, false, false},
		{"docs-site", "main", "js", "3 months ago", "Refresh the getting-started guide", 0, 0, 0, 0, 60, []int{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, false, false},
		{"cli-tools", "main", "rs", "2 days ago", "Add a --json output flag", 0, 0, 0, 0, 1, []int{2, 3, 5, 4, 6, 5, 7, 4, 6, 5, 8, 6}, false, false},
		{"search-index", "main", "go", "8 days ago", "Shard the index by tenant", 0, 12, 0, 0, 6, []int{1, 2, 0, 3, 1, 0, 2, 1, 0, 1, 0, 0}, false, false},
		{"image-resizer", "main", "rs", "12 days ago", "Stream resized images to avoid buffering", 0, 0, 0, 0, 13, []int{0, 1, 0, 2, 0, 1, 0, 0, 1, 0, 0, 0}, false, false},
		{"feature-flags", "main", "kt", "yesterday", "Add per-environment overrides", 0, 0, 2, 0, 1, []int{1, 0, 2, 3, 1, 2, 4, 3, 2, 5, 3, 4}, false, false},
		{"analytics-core", "main", "scala", "3 days ago", "Window sessions by inactivity gap", 3, 0, 0, 0, 2, []int{2, 1, 3, 2, 4, 3, 1, 2, 3, 4, 2, 3}, false, false},
		{"gateway", "main", "go", "6 days ago", "Add circuit breaker to upstream calls", 0, 0, 0, 0, 4, []int{0, 1, 2, 0, 1, 0, 2, 1, 0, 1, 2, 1}, false, false},
		{"user-profile", "main", "rb", "10 days ago", "Cache avatars at the edge", 0, 0, 0, 0, 8, []int{1, 0, 1, 0, 0, 1, 0, 2, 0, 1, 0, 0}, false, false},
		{"webhooks", "fix/retry-backoff", "go", "4 hours ago", "Add jittered exponential backoff", 0, 0, 1, 2, 1, []int{2, 3, 1, 4, 2, 5, 3, 4, 6, 3, 5, 4}, false, false},
		{"scheduler", "v2.3.0", "go", "5 weeks ago", "Tag the 2.3.0 release", 0, 0, 0, 0, 30, []int{1, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 0}, true, false},
		{"cache-layer", "main", "rs", "7 days ago", "Add an LRU eviction policy", 0, 0, 0, 0, 5, []int{0, 2, 1, 0, 1, 2, 0, 1, 0, 2, 1, 0}, false, true},
	}
}

// demoRepos expands the specs into full repo.Repo values, with display state
// computed exactly as a real scan would.
func demoRepos() []repo.Repo {
	specs := demoSpecs()
	repos := make([]repo.Repo, 0, len(specs))
	for _, s := range specs {
		r := repo.Repo{
			Name:          s.name,
			Path:          "/orchard-demo/" + s.name,
			Branch:        s.branch,
			DefaultBranch: "main",
			Upstream:      "origin/" + s.branch,
			HasUpstream:   !s.noUp,
			Dirty:         s.changed > 0,
			Ahead:         s.ahead,
			Behind:        s.behind,
			Detached:      s.detached,
			ChangedFiles:  s.changed,
			Stashes:       s.stashes,
			LastCommit:    s.rel + "\t" + s.subject,
			LastFetched:   time.Now().Add(-time.Duration(s.syncedH) * time.Hour),
			Activity:      s.activity,
		}
		// Per-repo Claude Code footprint (sessions, hours since last session). Mirrors
		// demoClaude(). acme-web is dirty + recent, so it shows the uncommitted flag.
		if cc, ok := map[string][2]int{
			"acme-web": {6, 3}, "payments-api": {3, 26}, "data-pipeline": {2, 96}, "cli-tools": {1, 2},
		}[s.name]; ok {
			r.CCSessions = cc[0]
			r.CCLast = time.Now().Add(-time.Duration(cc[1]) * time.Hour)
		}
		repos = append(repos, r.WithDisplay())
	}
	return repos
}

// demoLangs maps each demo repo path to its (authentic) language Stat.
func demoLangs() map[string]lang.Stat {
	out := map[string]lang.Stat{}
	for _, s := range demoSpecs() {
		if st, ok := lang.ByExtension(s.ext); ok {
			out["/orchard-demo/"+s.name] = st
		}
	}
	return out
}

// demoNew marks a couple of repos as having new commits since the last visit.
func demoNew() map[string]int {
	return map[string]int{
		"/orchard-demo/acme-web":     4,
		"/orchard-demo/payments-api": 1,
		"/orchard-demo/cli-tools":    2,
	}
}

// demoClaude is a fabricated Claude Code usage rollup for the pinned panel.
func demoClaude() claude.Usage {
	now := time.Now()
	repos := []claude.RepoUsage{
		{Name: "acme-web", Sessions: 6, Turns: 142, Last: now.Add(-3 * time.Hour)},
		{Name: "payments-api", Sessions: 3, Turns: 88, Last: now.Add(-26 * time.Hour)},
		{Name: "data-pipeline", Sessions: 2, Turns: 41, Last: now.Add(-4 * 24 * time.Hour)},
	}
	return claude.Usage{
		TotalSessions: 11,
		TotalTurns:    271,
		TotalTokens:   18_420_000,
		ReposUsed:     3,
		Models:        map[string]int{"opus-4.8": 233, "sonnet-4.6": 38},
		Repos:         repos,
		Last:          now.Add(-3 * time.Hour),
	}
}

// demoDiff returns a fabricated unified diff for the inline diff viewer.
func demoDiff() string {
	return `diff --git a/src/handlers/checkout.ts b/src/handlers/checkout.ts
index 8a1f2c3..b4d5e6f 100644
--- a/src/handlers/checkout.ts
+++ b/src/handlers/checkout.ts
@@ -22,7 +22,9 @@ export async function checkoutHandler(req: Request) {
   const cart = await loadCart(req.session)
-  const order = buildSummary(cart)
+  const order = buildCheckoutSummary(cart)
+  order.discount = applyPromo(cart, req.body.promoCode)
   return render("checkout", { order })
 }
diff --git a/src/components/SummaryCard.tsx b/src/components/SummaryCard.tsx
index 1122334..5566778 100644
--- a/src/components/SummaryCard.tsx
+++ b/src/components/SummaryCard.tsx
@@ -10,3 +10,4 @@ export function SummaryCard({ order }: Props) {
   return (
-    <div className="card">{order.total}</div>
+    <div className="card summary">{formatPrice(order.total)}</div>
   )
 }`
}

// demoGHStatus returns fabricated GitHub PR/CI status for a few demo repos.
func demoGHStatus() map[string]github.RepoStatus {
	return map[string]github.RepoStatus{
		"/orchard-demo/acme-web": {OpenPRs: 2, CIState: "failing", PRs: []github.PR{
			{Number: 142, Title: "Redesign the checkout summary card"},
			{Number: 138, Title: "Add saved cards to checkout"},
		}},
		"/orchard-demo/payments-api": {OpenPRs: 1, CIState: "passing", PRs: []github.PR{
			{Number: 57, Title: "Idempotency keys on the charge endpoint"},
		}},
		"/orchard-demo/data-pipeline":  {CIState: "pending"},
		"/orchard-demo/design-system":  {OpenPRs: 3, CIState: "passing"},
		"/orchard-demo/billing-worker": {CIState: "failing"},
	}
}

// demoSessions returns a fabricated Claude Code session history for the picker.
func demoSessions() []claude.Session {
	now := time.Now()
	return []claude.Session{
		{ID: "demo-0001-checkout-redesign", Title: "Redesign the checkout summary card", Model: "opus-4.8", Assistant: 42, Tokens: 3_120_000, Modified: now.Add(-3 * time.Hour)},
		{ID: "demo-0002-idempotency-keys", Title: "Add idempotency keys to the charge endpoint", Model: "opus-4.8", Assistant: 28, Tokens: 1_840_000, Modified: now.Add(-26 * time.Hour)},
		{ID: "demo-0003-flaky-auth-test", Title: "Investigate the flaky auth test", Model: "sonnet-4.6", Assistant: 15, Tokens: 720_000, Modified: now.Add(-2 * 24 * time.Hour)},
		{ID: "demo-0004-promo-code", Title: "Wire up the promo code field", Model: "opus-4.8", Assistant: 33, Tokens: 2_050_000, Modified: now.Add(-5 * 24 * time.Hour)},
		{ID: "demo-0005-price-helper", Title: "Extract a price formatting helper", Model: "sonnet-4.6", Assistant: 9, Tokens: 410_000, Modified: now.Add(-8 * 24 * time.Hour)},
	}
}

// demoDetailLangs returns the language breakdown shown in a demo repo's detail
// view (a single dominant language at 100%).
func demoDetailLangs(path string) []lang.Stat {
	if st, ok := demoLangs()[path]; ok {
		st.Pct = 100
		return []lang.Stat{st}
	}
	return nil
}

// demoDetail returns a fabricated detail view for a repo.
func demoDetail(r repo.Repo) orchardgit.DetailInfo {
	return orchardgit.DetailInfo{
		Branch:   r.Branch,
		Upstream: "origin/" + r.Branch,
		StatusLines: []string{
			" M src/handlers/checkout.ts",
			" M src/components/SummaryCard.tsx",
			"?? src/components/SummaryCard.test.tsx",
		},
		Commits: []orchardgit.Commit{
			{Hash: "a1b2c3d", Rel: "2 hours ago", Subject: "Redesign the checkout summary card", Author: "Jordan Lee"},
			{Hash: "9f8e7d6", Rel: "yesterday", Subject: "Extract price formatting helper", Author: "Sam Rivera"},
			{Hash: "4c5b6a7", Rel: "2 days ago", Subject: "Add empty-cart state", Author: "Jordan Lee"},
			{Hash: "1d2e3f4", Rel: "3 days ago", Subject: "Wire up the promo code field", Author: "Priya Nair"},
		},
		Graph: []orchardgit.GraphRow{
			{Rail: "*", IsCommit: true, Hash: "a1b2c3d", Rel: "2h", Subject: "Redesign the checkout summary card", Author: "Jordan Lee"},
			{Rail: "*", IsCommit: true, Hash: "9f8e7d6", Rel: "1d", Subject: "Extract price formatting helper", Author: "Sam Rivera"},
			{Rail: "|\\", IsCommit: false},
			{Rail: "* |", IsCommit: true, Hash: "4c5b6a7", Rel: "2d", Subject: "Add empty-cart state", Author: "Jordan Lee"},
			{Rail: "|/", IsCommit: false},
			{Rail: "*", IsCommit: true, Hash: "1d2e3f4", Rel: "3d", Subject: "Wire up the promo code field", Author: "Priya Nair"},
		},
		Remotes: []string{"origin\tgit@github.com:acme/" + r.Name + ".git"},
	}
}

// demoBranches returns a fabricated branch list for the switcher.
func demoBranches() []orchardgit.Branch {
	return []orchardgit.Branch{
		{Name: "feat/checkout-redesign", Current: true, Rel: "2 hours ago"},
		{Name: "main", Rel: "2 hours ago"},
		{Name: "fix/cart-total-rounding", Rel: "3 days ago"},
		{Name: "chore/bump-deps", Rel: "5 days ago"},
		{Name: "feat/saved-cards", Remote: true, Rel: "1 week ago"},
		{Name: "experiment/one-page-checkout", Remote: true, Rel: "2 weeks ago"},
		{Name: "release/2.3", Remote: true, Rel: "4 weeks ago"},
	}
}

// demoSearch returns fabricated matches for a query across demo repos.
func demoSearch(query string) []search.Result {
	q := query
	if q == "" {
		q = "checkout"
	}
	title := q
	if len(title) > 0 {
		title = strings.ToUpper(title[:1]) + title[1:]
	}
	mk := func(repo, path string, line int, text string) search.Match {
		return search.Match{Repo: repo, Path: "/orchard-demo/" + repo, File: path, Line: line, Text: text}
	}
	return []search.Result{
		{Repo: "acme-web", Path: "/orchard-demo/acme-web", Matches: []search.Match{
			mk("acme-web", "src/handlers/checkout.ts", 24, "export async function "+q+"Handler(req: Request) {"),
			mk("acme-web", "src/handlers/checkout.ts", 38, "  const order = await build"+q+"Summary(cart)"),
			mk("acme-web", "src/components/SummaryCard.tsx", 12, "// "+q+" summary, shown on the review step"),
		}},
		{Repo: "payments-api", Path: "/orchard-demo/payments-api", Matches: []search.Match{
			mk("payments-api", "internal/charge/charge.go", 57, "func (s *Service) "+q+"(ctx context.Context, o Order) error {"),
			mk("payments-api", "internal/charge/charge_test.go", 91, "func Test"+title+"(t *testing.T) {"),
		}},
		{Repo: "design-system", Path: "/orchard-demo/design-system", Matches: []search.Match{
			mk("design-system", "src/tokens/spacing.ts", 8, "export const "+q+"Gap = 16 // px"),
		}},
	}
}
