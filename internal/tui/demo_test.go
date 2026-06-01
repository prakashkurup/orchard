package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// fictionalRepos is the complete set of names demo mode may ever show. Demo data
// must stay within this set so a screenshot can never contain a real repo name.
var fictionalRepos = map[string]bool{
	"acme-web": true, "analytics-core": true, "auth-service": true, "billing-worker": true,
	"cache-layer": true, "cli-tools": true, "data-pipeline": true, "design-system": true,
	"docs-site": true, "feature-flags": true, "gateway": true, "image-resizer": true,
	"mobile-app": true, "notification-hub": true, "payments-api": true, "scheduler": true,
	"search-index": true, "user-profile": true, "webhooks": true,
}

// TestDemoDataIsFictional asserts every demo repo (and search result) is invented:
// a known name under the demo path. This is the guarantee that demo mode can never
// surface a real repository, so screenshots are always safe to publish.
func TestDemoDataIsFictional(t *testing.T) {
	repos := demoRepos()
	if len(repos) == 0 {
		t.Fatal("demoRepos() is empty")
	}
	for _, r := range repos {
		if !strings.HasPrefix(r.Path, "/orchard-demo/") {
			t.Errorf("demo repo %q has non-demo path %q", r.Name, r.Path)
		}
		if !fictionalRepos[r.Name] {
			t.Errorf("demo repo name %q is not in the fictional set", r.Name)
		}
	}
	for _, res := range demoSearch("query") {
		if !fictionalRepos[res.Repo] {
			t.Errorf("demo search result repo %q is not in the fictional set", res.Repo)
		}
	}
	for _, s := range demoSessions() {
		if strings.TrimSpace(s.Title) == "" {
			t.Error("demo session has an empty title")
		}
		if !strings.HasPrefix(s.ID, "demo-") {
			t.Errorf("demo session id %q should be a fictional demo- id", s.ID)
		}
	}
	for _, marker := range []string{"/Users/", "/home/", "Documents/GitHub"} {
		if strings.Contains(demoDiff(), marker) {
			t.Errorf("demo diff leaks a real-path marker %q", marker)
		}
	}
}

// renderedDemo joins the demo output of the views a user might screenshot.
func renderedDemo(t *testing.T) string {
	t.Helper()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Setenv("ORCHARD_DEMO", "1")
	dash, err := Preview("/x", 8, 150, 30, false)
	if err != nil {
		t.Fatal(err)
	}
	det, err := PreviewDetail("/x", 8, 150, 30, "acme-web")
	if err != nil {
		t.Fatal(err)
	}
	return ansi.Strip(dash + "\n" + det)
}

// TestDemoRendersNoRealPaths asserts demo output carries no real local paths
// (which would mean it accidentally scanned the filesystem) and is populated.
func TestDemoRendersNoRealPaths(t *testing.T) {
	out := renderedDemo(t)
	for _, marker := range []string{"/Users/", "/home/", "Documents/GitHub"} {
		if strings.Contains(out, marker) {
			t.Errorf("demo output contains real-path marker %q", marker)
		}
	}
	for _, want := range []string{"acme-web", "payments-api", "CHANGES", "ACTIVITY"} {
		if !strings.Contains(out, want) {
			t.Errorf("demo output missing %q", want)
		}
	}
}
