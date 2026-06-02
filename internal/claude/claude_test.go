package claude

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrettyModel(t *testing.T) {
	cases := map[string]string{
		"claude-opus-4-8-20251101": "opus-4.8",
		"claude-sonnet-4-6":        "sonnet-4.6",
		"claude-haiku-4-5":         "haiku-4.5",
		"":                         "",
	}
	for in, want := range cases {
		if got := PrettyModel(in); got != want {
			t.Errorf("PrettyModel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEncode(t *testing.T) {
	if got := encode("/Users/a.b/x"); got != "-Users-a-b-x" {
		t.Errorf("encode = %q, want -Users-a-b-x", got)
	}
}

func TestSummaryIncludesSubdirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	repoPath := "/Users/me/Documents/GitHub/foo"
	enc := encode(repoPath)
	proj := filepath.Join(home, ".claude", "projects")

	write := func(dir, name, content string) {
		d := filepath.Join(proj, dir)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// session at repo root + a session in a subdir cwd
	write(enc, "s1.jsonl", `{"type":"assistant","message":{"model":"claude-opus-4-8"}}`+"\n")
	write(enc+"-app", "s2.jsonl", `{"type":"assistant","message":{"model":"claude-sonnet-4-6"}}`+"\n")

	n, last := Summary(repoPath)
	if n != 2 {
		t.Fatalf("Summary sessions = %d, want 2 (root + subdir)", n)
	}
	if last.IsZero() {
		t.Fatal("expected a last-modified time")
	}

	ss := Sessions(repoPath, 0)
	if len(ss) != 2 {
		t.Fatalf("Sessions = %d, want 2", len(ss))
	}
	models := map[string]bool{}
	for _, s := range ss {
		models[PrettyModel(s.Model)] = true
		if s.Assistant != 1 {
			t.Errorf("assistant turns = %d, want 1", s.Assistant)
		}
	}
	if !models["opus-4.8"] || !models["sonnet-4.6"] {
		t.Fatalf("models parsed = %v", models)
	}
}

func TestSessionTitle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	repoPath := "/Users/me/Documents/GitHub/bar"
	dir := filepath.Join(home, ".claude", "projects", encode(repoPath))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// session with an ai-title (and two assistant turns carrying token usage)
	write("s1.jsonl", `{"type":"ai-title","aiTitle":"Add retry logic to the worker","sessionId":"s1"}`+"\n"+
		`{"type":"assistant","message":{"model":"claude-opus-4-8","usage":{"input_tokens":100,"output_tokens":50,"cache_read_input_tokens":10,"cache_creation_input_tokens":5}}}`+"\n"+
		`{"type":"assistant","message":{"model":"claude-opus-4-8","usage":{"input_tokens":200,"output_tokens":80}}}`+"\n")
	// session with only a last-prompt -> title falls back to its first line
	write("s2.jsonl", `{"type":"last-prompt","lastPrompt":"fix the flaky test\nplease","sessionId":"s2"}`+"\n"+
		`{"type":"assistant","message":{"model":"claude-sonnet-4-6"}}`+"\n")

	byID := map[string]Session{}
	for _, s := range Sessions(repoPath, 0) {
		byID[s.ID] = s
	}
	if got := byID["s1"].Title; got != "Add retry logic to the worker" {
		t.Errorf("s1 title = %q, want the ai-title", got)
	}
	if got := byID["s1"].Assistant; got != 2 {
		t.Errorf("s1 turns = %d, want 2", got)
	}
	if got := byID["s1"].Tokens; got != 445 { // (100+50+10+5) + (200+80)
		t.Errorf("s1 tokens = %d, want 445", got)
	}
	if got := byID["s2"].Title; got != "fix the flaky test" {
		t.Errorf("s2 title = %q, want the first line of the last prompt", got)
	}
}

func TestAggregate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	repoPath := "/Users/me/Documents/GitHub/bar"
	d := filepath.Join(home, ".claude", "projects", encode(repoPath))
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(d, "a.jsonl"), []byte(`{"type":"assistant","message":{"model":"claude-opus-4-8"}}`+"\n"), 0o644)

	u := Aggregate([]Target{{Name: "bar", Path: repoPath}})
	if u.TotalSessions != 1 || u.ReposUsed != 1 || u.TotalTurns != 1 {
		t.Fatalf("aggregate = %+v", u)
	}
	if u.Models["opus-4.8"] != 1 {
		t.Fatalf("models = %v", u.Models)
	}
}

func TestProjectsRootHonorsConfigDir(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/custom/cc")
	if got := ProjectsRoot(); got != filepath.Join("/custom/cc", "projects") {
		t.Fatalf("with CLAUDE_CONFIG_DIR set, ProjectsRoot = %q", got)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "") // empty falls back to HOME/.claude
	if got, want := ProjectsRoot(), filepath.Join(home, ".claude", "projects"); got != want {
		t.Fatalf("fallback = %q, want %q", got, want)
	}
}
