package claude

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTouchMap(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")

	repoPath := "/work/payments"
	dir := filepath.Join(home, ".claude", "projects", encode(repoPath))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Edit","input":{"file_path":"/work/payments/internal/auth/token.go"}}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Edit","input":{"file_path":"/work/payments/internal/auth/token.go"}}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/work/payments/internal/store/pg.go"}}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/etc/hosts"}}]}}`,                          // outside repo -> ignored
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"ls"}}]}}`,                                    // no file -> ignored
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"NotebookEdit","input":{"notebook_path":"/work/payments/nb.ipynb"}}]}}`, // notebook write
	}
	content := ""
	for _, l := range lines {
		content += l + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "s1.jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got := TouchMap(repoPath, 0)
	if len(got) != 3 {
		t.Fatalf("want 3 in-repo files (out-of-repo and Bash dropped), got %d: %+v", len(got), got)
	}
	// edited files sort before read-only ones; token.go (2 edits) leads.
	if got[0].Path != "internal/auth/token.go" || got[0].Writes != 2 || !got[0].Wrote() {
		t.Errorf("first should be edited token.go x2, got %+v", got[0])
	}
	if !got[1].Wrote() { // the notebook write also sorts ahead of the read
		t.Errorf("second should be a write (notebook), got %+v", got[1])
	}
	last := got[len(got)-1]
	if last.Path != "internal/store/pg.go" || last.Reads != 1 || last.Wrote() {
		t.Errorf("read-only pg.go should sort last, got %+v", last)
	}
}

func TestRepoRel(t *testing.T) {
	repo := "/work/payments"
	prefix := repo + "/"
	cases := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{"/work/payments/a/b.go", "a/b.go", true},
		{"/work/payments", "payments", true},
		{"/elsewhere/x.go", "", false},
		{"", "", false},
		{"relative/path.go", "relative/path.go", true},
	}
	for _, c := range cases {
		got, ok := repoRel(c.in, repo, prefix)
		if got != c.want || ok != c.wantOK {
			t.Errorf("repoRel(%q) = (%q,%v), want (%q,%v)", c.in, got, ok, c.want, c.wantOK)
		}
	}
}
