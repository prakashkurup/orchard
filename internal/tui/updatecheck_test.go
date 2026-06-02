package tui

import (
	"regexp"
	"strings"
	"testing"
)

func TestHeaderShowsUpdateNotice(t *testing.T) {
	ansi := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	m := newModel("root", 4)
	m.width, m.height = 120, 30
	m.repos = sampleRepos()
	m.resize()
	m.loading = false
	m.status = ""
	m.updateTag = "v9.9.9"
	out := ansi.ReplaceAllString(m.headerView(m.innerWidth()), "")
	if !strings.Contains(out, "v9.9.9") || !strings.Contains(out, "orchard update") {
		t.Fatalf("header should show the update notice, got:\n%s", out)
	}
}
