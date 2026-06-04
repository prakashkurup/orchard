package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prakashkurup/orchard/internal/agent"
)

// writeRollout creates a rollout JSONL under home/sessions/<date>/ for a session
// with the given id, cwd, and pre-built record lines.
func writeRollout(t *testing.T, home, id, cwd string, lines []string) {
	t.Helper()
	dir := filepath.Join(home, "sessions", "2026", "06", "03")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := `{"type":"session_meta","payload":{"id":"` + id + `","cwd":"` + cwd + `"}}`
	all := append([]string{meta}, lines...)
	body := ""
	for _, l := range all {
		body += l + "\n"
	}
	path := filepath.Join(dir, "rollout-2026-06-03T19-24-42-"+id+".jsonl")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSessionsParse(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	repo := "/work/acme-web"

	writeRollout(t, home, "019e0000-0000-7000-0000-000000000001", repo, []string{
		`{"type":"turn_context","payload":{"turn_id":"t1","model":"gpt-5.5"}}`,
		`{"type":"event_msg","payload":{"type":"agent_message"}}`,
		`{"type":"event_msg","payload":{"type":"agent_message"}}`,
		`{"type":"event_msg","payload":{"type":"agent_message"}}`,
		`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"total_tokens":1000}}}}`,
		`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"total_tokens":2500}}}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"text","text":"Add a feature\nplease"}]}}`,
	})
	// session_index supplies the title for this id
	idx := `{"id":"019e0000-0000-7000-0000-000000000001","thread_name":"Add the feature"}` + "\n"
	if err := os.WriteFile(filepath.Join(home, "session_index.jsonl"), []byte(idx), 0o644); err != nil {
		t.Fatal(err)
	}

	n, last := Summary(repo)
	if n != 1 || last.IsZero() {
		t.Fatalf("Summary = (%d, %v), want (1, non-zero)", n, last)
	}
	ss := Sessions(repo, 10)
	if len(ss) != 1 {
		t.Fatalf("Sessions returned %d, want 1", len(ss))
	}
	s := ss[0]
	if s.Model != "gpt-5.5" {
		t.Errorf("model = %q, want gpt-5.5", s.Model)
	}
	if s.Assistant != 3 {
		t.Errorf("turns = %d, want 3", s.Assistant)
	}
	if s.Tokens != 2500 {
		t.Errorf("tokens = %d, want 2500 (cumulative max)", s.Tokens)
	}
	if s.Title != "Add the feature" {
		t.Errorf("title = %q, want the session_index thread_name", s.Title)
	}
}

func TestTitleFallbackToFirstUserMessage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	repo := "/work/acme-web"
	writeRollout(t, home, "019e0000-0000-7000-0000-000000000002", repo, []string{
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"text","text":"Fix the flaky test\nin CI"}]}}`,
	})
	ss := Sessions(repo, 10)
	if len(ss) != 1 || ss[0].Title != "Fix the flaky test" {
		t.Fatalf("title fallback = %q, want first user line", ss[0].Title)
	}
}

func TestRepoScopingAndAggregate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	web := "/work/acme-web"
	api := "/work/payments-api"
	writeRollout(t, home, "019e0000-0000-7000-0000-00000000000a", web, []string{
		`{"type":"event_msg","payload":{"type":"agent_message"}}`,
	})
	// a session run from a SUBDIR of web still belongs to web
	writeRollout(t, home, "019e0000-0000-7000-0000-00000000000b", web+"/internal", []string{
		`{"type":"event_msg","payload":{"type":"agent_message"}}`,
	})
	writeRollout(t, home, "019e0000-0000-7000-0000-00000000000c", api, []string{
		`{"type":"event_msg","payload":{"type":"agent_message"}}`,
	})

	if n, _ := Summary(web); n != 2 {
		t.Errorf("web sessions = %d, want 2 (incl. the subdir session)", n)
	}
	if n, _ := Summary(api); n != 1 {
		t.Errorf("api sessions = %d, want 1", n)
	}
	u := Aggregate([]agent.Target{{Name: "web", Path: web}, {Name: "api", Path: api}})
	if u.TotalSessions != 3 || u.ReposUsed != 2 {
		t.Errorf("aggregate sessions=%d repos=%d, want 3 and 2", u.TotalSessions, u.ReposUsed)
	}
}

func TestTouchMapFromPatches(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	repo := "/work/acme-web"
	writeRollout(t, home, "019e0000-0000-7000-0000-0000000000f1", repo, []string{
		`{"type":"event_msg","payload":{"type":"patch_apply_end","success":true,"changes":{"/work/acme-web/main.go":{"type":"update"},"/work/acme-web/internal/x.go":{"type":"add"}}}}`,
		`{"type":"event_msg","payload":{"type":"patch_apply_end","success":true,"changes":{"/work/acme-web/main.go":{"type":"update"}}}}`,
		`{"type":"event_msg","payload":{"type":"patch_apply_end","success":false,"changes":{"/work/acme-web/skip.go":{"type":"update"}}}}`,
		`{"type":"event_msg","payload":{"type":"patch_apply_end","success":true,"changes":{"/other/repo/z.go":{"type":"update"}}}}`,
	})
	tm := TouchMap(repo, 10)
	got := map[string]int{}
	for _, f := range tm {
		got[f.Path] = f.Writes
	}
	if got["main.go"] != 2 {
		t.Errorf("main.go writes = %d, want 2", got["main.go"])
	}
	if got["internal/x.go"] != 1 {
		t.Errorf("internal/x.go writes = %d, want 1", got["internal/x.go"])
	}
	if _, ok := got["skip.go"]; ok {
		t.Error("a failed patch should not count")
	}
	if len(tm) != 2 {
		t.Errorf("touched files = %d, want 2 (out-of-repo path excluded)", len(tm))
	}
	if !tm[0].Wrote() {
		t.Error("touched files should be marked as edits")
	}
}

func TestSearchSessions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	repo := "/work/acme-web"
	writeRollout(t, home, "019e0000-0000-7000-0000-0000000000f2", repo, []string{
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"text","text":"please add a RealmResolver to the service"}]}}`,
	})
	hits := SearchSessions([]agent.Target{{Name: "acme-web", Path: repo}}, "realmresolver", 10)
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(hits))
	}
	if !strings.Contains(strings.ToLower(hits[0].Snippet), "realmresolver") {
		t.Errorf("snippet should contain the match: %q", hits[0].Snippet)
	}
	if none := SearchSessions([]agent.Target{{Name: "acme-web", Path: repo}}, "nonexistent-xyz", 10); len(none) != 0 {
		t.Errorf("expected no hits, got %d", len(none))
	}
}

func TestNoCodexHome(t *testing.T) {
	t.Setenv("CODEX_HOME", filepath.Join(t.TempDir(), "does-not-exist"))
	if n, _ := Summary("/whatever"); n != 0 {
		t.Errorf("missing home should yield 0 sessions, got %d", n)
	}
}
