package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/prakashkurup/orchard/internal/repo"
)

func TestInstrStatePredicates(t *testing.T) {
	cases := []struct {
		s         instrState
		needsWire bool
		canWire   bool
	}{
		{instrState{}, false, false},
		{instrState{hasClaude: true}, false, false},
		{instrState{hasAgents: true}, true, true},                                   // agents only → fixable
		{instrState{hasClaude: true, hasAgents: true}, true, false},                 // both, no import → flag, not auto-fixable
		{instrState{hasClaude: true, hasAgents: true, imports: true}, false, false}, // wired
	}
	for i, c := range cases {
		if c.s.needsWiring() != c.needsWire {
			t.Errorf("case %d needsWiring=%v want %v", i, c.s.needsWiring(), c.needsWire)
		}
		if c.s.canWire() != c.canWire {
			t.Errorf("case %d canWire=%v want %v", i, c.s.canWire(), c.canWire)
		}
	}
}

func TestDetectInstrAndWire(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# agents\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// agents-only → canWire
	if s := detectInstr(dir); !s.canWire() {
		t.Fatalf("expected canWire for agents-only, got %+v", s)
	}

	// wire it: a CLAUDE.md importing AGENTS.md should appear
	msg := wireInstrCmd([]repo.Repo{{Path: dir}})().(wireInstrMsg)
	if msg.wired != 1 || msg.err != "" {
		t.Fatalf("wire result: %+v", msg)
	}
	data, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil || string(data) != agentsImport {
		t.Fatalf("CLAUDE.md = %q err=%v", string(data), err)
	}

	// now detect should see both + import, and not need wiring
	if s := detectInstr(dir); !s.imports || s.needsWiring() {
		t.Fatalf("after wiring expected imported + not-needy, got %+v", s)
	}

	// re-wiring must NOT overwrite the existing CLAUDE.md
	msg2 := wireInstrCmd([]repo.Repo{{Path: dir}})().(wireInstrMsg)
	if msg2.wired != 0 || msg2.skipped != 1 {
		t.Fatalf("re-wire should skip existing CLAUDE.md, got %+v", msg2)
	}
}

func TestPassQuickNeedsInstr(t *testing.T) {
	m := model{
		quick: filterNeedsInstr,
		instructionsByPath: map[string]instrState{
			"/a": {hasAgents: true},                                 // agents only → blind
			"/b": {hasClaude: true, hasAgents: true, imports: true}, // wired → not blind
			"/c": {},                                                // nothing → blind (broadened)
			"/d": {hasClaude: true},                                 // claude only → not blind
		},
	}
	for _, p := range []string{"/a", "/c"} {
		if !m.passQuick(repo.Repo{Path: p}) {
			t.Errorf("blind repo %s should pass needs-md", p)
		}
	}
	for _, p := range []string{"/b", "/d"} {
		if m.passQuick(repo.Repo{Path: p}) {
			t.Errorf("repo %s with a CLAUDE.md should not pass needs-md", p)
		}
	}
}
