package agentcfg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unparseable .mcp.json: %v\n%s", err, data)
	}
	return m
}

func TestEnsureClaudeMCPCreatesAndIsIdempotent(t *testing.T) {
	repo := t.TempDir()
	bin := "/usr/local/bin/orchard"

	changed, err := EnsureClaudeMCP(repo, []string{repo}, bin)
	if err != nil || !changed {
		t.Fatalf("first call: changed=%v err=%v, want changed=true", changed, err)
	}
	m := readJSON(t, filepath.Join(repo, ".mcp.json"))
	srv, _ := m["mcpServers"].(map[string]any)
	orch, _ := srv["orchard"].(map[string]any)
	if orch["command"] != bin {
		t.Errorf("command = %v, want %v", orch["command"], bin)
	}
	args, _ := orch["args"].([]any)
	repoAbs, _ := filepath.Abs(repo)
	if len(args) != 3 || args[0] != "mcp" || args[1] != "--repo" || args[2] != repoAbs {
		t.Errorf("args = %v, want [mcp --repo %s]", args, repoAbs)
	}

	// Idempotent: a second call changes nothing.
	changed, err = EnsureClaudeMCP(repo, []string{repo}, bin)
	if err != nil || changed {
		t.Errorf("second call: changed=%v err=%v, want changed=false", changed, err)
	}
}

func TestEnsureClaudeMCPCrossRepo(t *testing.T) {
	primary := t.TempDir()
	other := t.TempDir()
	if _, err := EnsureClaudeMCP(primary, []string{primary, other}, "/bin/orchard"); err != nil {
		t.Fatal(err)
	}
	m := readJSON(t, filepath.Join(primary, ".mcp.json"))
	orch := m["mcpServers"].(map[string]any)["orchard"].(map[string]any)
	args, _ := orch["args"].([]any)
	pAbs, _ := filepath.Abs(primary)
	oAbs, _ := filepath.Abs(other)
	want := []any{"mcp", "--repo", pAbs, "--repo", oAbs}
	if len(args) != len(want) {
		t.Fatalf("args = %v, want %v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("args[%d] = %v, want %v", i, args[i], want[i])
		}
	}
}

func TestEnsureCodexMCP(t *testing.T) {
	repo := t.TempDir()
	// Pre-existing config must be preserved.
	codexDir := filepath.Join(repo, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte("model = \"o4\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	changed, err := EnsureCodexMCP(repo, []string{repo}, "/bin/orchard")
	if err != nil || !changed {
		t.Fatalf("first call: changed=%v err=%v, want changed=true", changed, err)
	}
	data, _ := os.ReadFile(filepath.Join(codexDir, "config.toml"))
	s := string(data)
	repoAbs, _ := filepath.Abs(repo)
	for _, want := range []string{"model = \"o4\"", "[mcp_servers.orchard]", `command = "/bin/orchard"`, repoAbs, `"--repo"`} {
		if !strings.Contains(s, want) {
			t.Errorf("config.toml missing %q:\n%s", want, s)
		}
	}

	// Idempotent: second call leaves it unchanged.
	changed, err = EnsureCodexMCP(repo, []string{repo}, "/bin/orchard")
	if err != nil || changed {
		t.Errorf("second call: changed=%v err=%v, want changed=false", changed, err)
	}
}

func TestEnsureCodexMCPRefreshesStaleOrchardBlock(t *testing.T) {
	repo := t.TempDir()
	other := t.TempDir()
	codexDir := filepath.Join(repo, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(codexDir, "config.toml")
	stale := `model = "o4"

[mcp_servers.other]
command = "other"

[mcp_servers.orchard]
command = "/old/orchard"
args = ["mcp", "--repo", "/old/repo"]

[projects."/tmp/example"]
trusted = true
`
	if err := os.WriteFile(path, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}

	changed, err := EnsureCodexMCP(repo, []string{repo, other}, "/new/orchard")
	if err != nil || !changed {
		t.Fatalf("refresh call: changed=%v err=%v, want changed=true", changed, err)
	}
	data, _ := os.ReadFile(path)
	s := string(data)
	repoAbs, _ := filepath.Abs(repo)
	otherAbs, _ := filepath.Abs(other)
	for _, want := range []string{
		`model = "o4"`,
		`[mcp_servers.other]`,
		`[projects."/tmp/example"]`,
		`command = "/new/orchard"`,
		repoAbs,
		otherAbs,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("updated config missing %q:\n%s", want, s)
		}
	}
	for _, stalePart := range []string{`/old/orchard`, `/old/repo`} {
		if strings.Contains(s, stalePart) {
			t.Errorf("updated config still contains stale %q:\n%s", stalePart, s)
		}
	}

	changed, err = EnsureCodexMCP(repo, []string{repo, other}, "/new/orchard")
	if err != nil || changed {
		t.Errorf("second refresh call: changed=%v err=%v, want changed=false", changed, err)
	}
}

func TestEnsureClaudeMCPMergesNotClobbers(t *testing.T) {
	repo := t.TempDir()
	// Pre-existing config with another server and an unrelated top-level key.
	pre := `{
	  "mcpServers": { "other": { "type": "stdio", "command": "other-tool" } },
	  "someOtherSetting": true
	}`
	if err := os.WriteFile(filepath.Join(repo, ".mcp.json"), []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := EnsureClaudeMCP(repo, []string{repo}, "/bin/orchard"); err != nil {
		t.Fatal(err)
	}
	m := readJSON(t, filepath.Join(repo, ".mcp.json"))
	if m["someOtherSetting"] != true {
		t.Errorf("unrelated top-level key was lost: %v", m["someOtherSetting"])
	}
	srv, _ := m["mcpServers"].(map[string]any)
	if _, ok := srv["other"]; !ok {
		t.Error("pre-existing 'other' server was clobbered")
	}
	if _, ok := srv["orchard"]; !ok {
		t.Error("'orchard' server was not added")
	}
}
