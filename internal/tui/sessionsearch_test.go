package tui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/prakashkurup/orchard/internal/claude"
)

func TestSessionSearchViewRenders(t *testing.T) {
	ansi := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	m := newModel("root", 4)
	m.width, m.height = 120, 30
	m.resize()
	m.mode = modeSessionSearch
	m.sessionSearchQuery = "idempotent"
	m.sessionSearchResults = []claude.SessionHit{
		{RepoName: "payments-api", RepoPath: "/x/payments-api", ID: "abc12345ef", Title: "Add idempotency keys", Snippet: "…make the charge idempotent…", Modified: time.Now().Add(-time.Hour)},
	}
	out := ansi.ReplaceAllString(m.View(), "")
	if !strings.Contains(out, "payments-api") {
		t.Fatalf("expected the result repo in the view, got:\n%s", out)
	}
	if !strings.Contains(out, "Add idempotency keys") {
		t.Error("expected the result title in the view")
	}
	for _, ln := range strings.Split(out, "\n") {
		if w := len([]rune(ln)); w > m.width {
			t.Fatalf("line exceeds width %d: %q", m.width, ln)
		}
	}
}
