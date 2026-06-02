package tui

import (
	"testing"
	"time"

	"github.com/prakashkurup/orchard/internal/repo"
)

func TestEnvPrefix(t *testing.T) {
	if got := envPrefix(nil); got != "" {
		t.Errorf("envPrefix(nil) = %q, want empty", got)
	}
	if got := envPrefix([]string{"CLAUDE_X=1"}); got != "env 'CLAUDE_X=1' " {
		t.Errorf("envPrefix one = %q", got)
	}
	if got := envPrefix([]string{"A=1", "B=2"}); got != "env 'A=1' 'B=2' " {
		t.Errorf("envPrefix two = %q", got)
	}
}

func TestAddDirMemoryEnv(t *testing.T) {
	t.Setenv("ORCHARD_ADDDIR_MEMORY", "")
	if env := addDirMemoryEnv(); len(env) != 1 || env[0] != claudeAddDirMemoryEnv {
		t.Errorf("default should enable add-dir memory, got %v", env)
	}
	for _, off := range []string{"0", "false", "no", "off", "OFF"} {
		t.Setenv("ORCHARD_ADDDIR_MEMORY", off)
		if env := addDirMemoryEnv(); env != nil {
			t.Errorf("ORCHARD_ADDDIR_MEMORY=%q should disable, got %v", off, env)
		}
	}
}

func TestPassQuickRiskAndAITouched(t *testing.T) {
	clean := repo.Repo{Display: repo.DisplayClean}
	dirty := repo.Repo{Dirty: true}
	ahead := repo.Repo{Ahead: 2}
	stashed := repo.Repo{Stashes: 1}
	recent := repo.Repo{CCLast: time.Now().Add(-2 * time.Hour)}
	stale := repo.Repo{CCLast: time.Now().Add(-30 * 24 * time.Hour)}
	never := repo.Repo{}

	risk := model{quick: filterRisk}
	for _, r := range []repo.Repo{dirty, ahead, stashed} {
		if !risk.passQuick(r) {
			t.Errorf("at-risk should include %+v", r)
		}
	}
	if risk.passQuick(clean) {
		t.Error("at-risk should exclude a clean repo")
	}

	ai := model{quick: filterAITouched}
	if !ai.passQuick(recent) {
		t.Error("ai-touched should include a recently-Claude'd repo")
	}
	if ai.passQuick(stale) || ai.passQuick(never) {
		t.Error("ai-touched should exclude stale / never-Claude'd repos")
	}
}

func TestRepoMatchesPrefixes(t *testing.T) {
	r := repo.Repo{Name: "payments-api", Branch: "feat/checkout"}
	cases := []struct {
		q    string
		want bool
	}{
		{"payments", true},         // bare: name
		{"checkout", true},         // bare: branch
		{"branch:feat", true},      // branch prefix matches branch
		{"branch:payments", false}, // branch prefix must NOT match name
		{"name:payments", true},    // name prefix matches name
		{"name:feat", false},       // name prefix must NOT match branch
	}
	for _, c := range cases {
		if got := repoMatches(r, c.q); got != c.want {
			t.Errorf("repoMatches(%q) = %v, want %v", c.q, got, c.want)
		}
	}
}
