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

func TestAggregate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
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
