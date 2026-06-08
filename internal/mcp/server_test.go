package mcp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prakashkurup/orchard/internal/graph"
)

func buildRepo(t *testing.T, files map[string]string) *graph.Graph {
	t.Helper()
	repo := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitInit(t, repo)
	g, err := graph.Open(filepath.Join(t.TempDir(), "g.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { g.Close() })
	if _, err := g.Build(context.Background(), repo, graph.DefaultRegistry()); err != nil {
		t.Fatal(err)
	}
	return g
}

// TestMCPToolsOverProtocol serves TWO repos over an in-memory MCP transport and
// exercises every tool through a real MCP client — proving the Phase-4 gate and
// the Phase-7 cross-repo merge (who_calls finds callers in both repos).
func TestMCPToolsOverProtocol(t *testing.T) {
	r1 := buildRepo(t, map[string]string{
		"auth.go": "package p\n\nfunc Login() { validate() }\n\nfunc validate() {}\n",
		"api.go":  "package p\n\nfunc Handle() { Login() }\n",
	})
	r2 := buildRepo(t, map[string]string{
		"svc.go": "package q\n\nfunc Login() {}\n\nfunc Worker() { Login() }\n",
	})
	repos := []RepoGraph{NewRepoGraph("r1", r1), NewRepoGraph("r2", r2)}

	ctx := context.Background()
	clientT, serverT := sdk.NewInMemoryTransports()
	ss, err := newServer(repos).Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	lt, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(lt.Tools) != 6 {
		t.Errorf("ListTools = %d tools, want 6", len(lt.Tools))
	}

	call := func(name string, args map[string]any) string {
		res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("call %s: %v", name, err)
		}
		if res.IsError {
			t.Fatalf("tool %s returned IsError; content=%+v", name, res.Content)
		}
		var sb strings.Builder
		for _, c := range res.Content {
			if tc, ok := c.(*sdk.TextContent); ok {
				sb.WriteString(tc.Text)
			}
		}
		return sb.String()
	}

	// Cross-repo: who_calls(Login) must surface callers from BOTH repos, tagged.
	wc := call("who_calls", map[string]any{"name": "Login"})
	for _, want := range []string{"Handle", "Worker", "r1", "r2"} {
		if !strings.Contains(wc, want) {
			t.Errorf("who_calls(Login) missing %q (cross-repo merge):\n%s", want, wc)
		}
	}

	// Remaining tools return sensible content.
	checks := []struct{ name, args, want string }{
		{"find_definition", "validate", "auth.go"},
		{"blast_radius", "validate", "Handle"},
		{"search_symbols", "Log", "Login"},
	}
	for _, c := range checks {
		key := "name"
		if c.name == "search_symbols" {
			key = "query"
		}
		if got := call(c.name, map[string]any{key: c.args}); !strings.Contains(got, c.want) {
			t.Errorf("%s missing %q:\n%s", c.name, c.want, got)
		}
	}
	if s := call("repo_map", map[string]any{}); !strings.Contains(s, "freshness") || !strings.Contains(s, "trust") || !strings.Contains(s, `"language": "Go"`) {
		t.Errorf("repo_map missing freshness/trust:\n%s", s)
	}
	if s := call("status", map[string]any{}); !strings.Contains(s, "r1") || !strings.Contains(s, "r2") || !strings.Contains(s, "coverage") || !strings.Contains(s, "stale") || !strings.Contains(s, "trust") {
		t.Errorf("status should list both repos with readiness metadata:\n%s", s)
	}
}

func TestCrossRepoSortsBeforeTruncating(t *testing.T) {
	hits := []defHit{
		{Repo: "z", DefRow: graph.DefRow{Name: "Low", Path: "z.go", Line: 1, Rank: 0.1}},
		{Repo: "a", DefRow: graph.DefRow{Name: "High", Path: "a.go", Line: 1, Rank: 0.9}},
	}
	sortDefHits(hits)
	if hits[0].Name != "High" {
		t.Fatalf("top hit = %+v, want highest rank before truncation", hits[0])
	}

	callers := []callerHit{
		{Repo: "z", CallerRow: graph.CallerRow{Caller: "Low", Path: "z.go", Line: 2, Rank: 0.1}},
		{Repo: "a", CallerRow: graph.CallerRow{Caller: "High", Path: "a.go", Line: 2, Rank: 0.9}},
	}
	sortCallerHits(callers)
	if callers[0].Caller != "High" {
		t.Fatalf("top caller = %+v, want highest rank before truncation", callers[0])
	}
}

func TestRepoGraphStatusState(t *testing.T) {
	g := buildRepo(t, map[string]string{
		"main.go": "package p\n\nfunc Main() {}\n",
	})
	r := NewRepoGraph("repo", g)
	r.SetIndexing(true)
	r.SetError(os.ErrNotExist)

	status := freshAll([]RepoGraph{r})
	for _, want := range []string{"indexing", "error="} {
		if !strings.Contains(status, want) {
			t.Fatalf("freshAll missing %q: %s", want, status)
		}
	}
	trust := trustAll([]RepoGraph{r})
	if len(trust) != 1 || len(trust[0].Languages) == 0 || trust[0].Languages[0].Language != "Go" {
		t.Fatalf("trustAll should expose per-language trust labels: %+v", trust)
	}
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init"}, {"add", "-A"},
		{"-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-m", "init"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_TERMINAL_PROMPT=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}
