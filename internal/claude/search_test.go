package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchSessions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")

	repoPath := "/work/acme"
	dir := filepath.Join(home, ".claude", "projects", encode(repoPath))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.jsonl", `{"type":"ai-title","aiTitle":"Charge retries"}`+"\n"+
		`{"type":"user","message":{"content":"we should make the charge idempotent with an Idempotency-Key"}}`+"\n")
	write("b.jsonl", `{"type":"user","message":{"content":"unrelated work on the navbar"}}`+"\n")

	targets := []Target{{Name: "acme", Path: repoPath}}

	hits := SearchSessions(targets, "idempotent", 0)
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d (%v)", len(hits), hits)
	}
	h := hits[0]
	if h.ID != "a" || h.RepoName != "acme" {
		t.Errorf("hit ID/repo wrong: %+v", h)
	}
	if h.Title != "Charge retries" {
		t.Errorf("title = %q, want the ai-title", h.Title)
	}
	if !strings.Contains(strings.ToLower(h.Snippet), "idempotent") {
		t.Errorf("snippet should contain the match: %q", h.Snippet)
	}

	// case-insensitive
	if got := SearchSessions(targets, "IDEMPOTENT", 0); len(got) != 1 {
		t.Errorf("case-insensitive search should match, got %d", len(got))
	}
	// no match
	if got := SearchSessions(targets, "kubernetes", 0); len(got) != 0 {
		t.Errorf("expected no hits, got %d", len(got))
	}
	// empty query
	if got := SearchSessions(targets, "   ", 0); got != nil {
		t.Errorf("empty query should return nil, got %v", got)
	}
}

func TestCleanSnippetStripsControl(t *testing.T) {
	in := "a" + string(rune(0x1b)) + "[31mb" + string(rune(0x9b)) + "c" + string(rune(0x07)) + "d"
	got := cleanSnippet(in)
	for _, r := range got {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			t.Fatalf("control byte survived cleanSnippet: %q (%U)", got, r)
		}
	}
}

func TestSnippetAroundRuneSafe(t *testing.T) {
	// "世" (3 bytes) sits so the window start lands mid-rune; the snippet must
	// not cut it (no U+FFFD) and must still contain the match.
	line := "世" + strings.Repeat("x", 47) + "needle tail"
	idx := strings.Index(line, "needle")
	snip := snippetAround(line, idx, len("needle"))
	if strings.ContainsRune(snip, '�') {
		t.Fatalf("snippet cut a multibyte rune: %q", snip)
	}
	if !strings.Contains(snip, "needle") {
		t.Fatalf("snippet should contain the match: %q", snip)
	}
}
