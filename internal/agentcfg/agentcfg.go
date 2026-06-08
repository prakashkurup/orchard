// Package agentcfg wires orchard's code-graph MCP server into an AI agent's
// per-project config, so launching the agent from orchard makes the graph
// available automatically. It merges into existing config rather than
// overwriting it, and is idempotent.
package agentcfg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// serverName is the key orchard owns inside the agent's MCP server map.
const serverName = "orchard"

// EnsureClaudeMCP makes sure configRepo/.mcp.json contains an "orchard" stdio
// MCP server that runs `<orchardBin> mcp --repo <r1> --repo <r2> …` over the
// given repos (one repo for a normal launch; several for a cross-repo session).
// It preserves any other servers and top-level keys, and reports whether it
// changed the file (false if already correct). Claude Code reads .mcp.json.
func EnsureClaudeMCP(configRepo string, repos []string, orchardBin string) (changed bool, err error) {
	cfgAbs, err := filepath.Abs(configRepo)
	if err != nil {
		return false, err
	}
	args := []any{"mcp"}
	for _, r := range repos {
		rAbs, err := filepath.Abs(r)
		if err != nil {
			return false, err
		}
		args = append(args, "--repo", rAbs)
	}
	path := filepath.Join(cfgAbs, ".mcp.json")

	root := map[string]any{}
	if data, rerr := os.ReadFile(path); rerr == nil && len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, &root); err != nil {
			return false, err // don't clobber a file we can't parse
		}
	}

	servers, _ := root["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}

	want := map[string]any{
		"type":    "stdio",
		"command": orchardBin,
		"args":    args,
	}
	if existing, ok := servers[serverName].(map[string]any); ok && sameServer(existing, want) {
		return false, nil
	}
	servers[serverName] = want
	root["mcpServers"] = servers

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return false, err
	}
	out = append(out, '\n')
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// sameServer compares two server entries by their canonical JSON (map keys are
// sorted by json.Marshal, so this is order-independent).
func sameServer(a, b map[string]any) bool {
	ab, err1 := json.Marshal(a)
	bb, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && bytes.Equal(ab, bb)
}

// EnsureCodexMCP makes sure configRepo/.codex/config.toml contains an "orchard"
// MCP server running `<orchardBin> mcp --repo <r1> …`. Codex config is TOML
// ([mcp_servers.<name>]); to avoid a TOML dependency this only replaces the
// orchard-owned table and preserves every other table/key.
func EnsureCodexMCP(configRepo string, repos []string, orchardBin string) (changed bool, err error) {
	cfgAbs, err := filepath.Abs(configRepo)
	if err != nil {
		return false, err
	}
	dir := filepath.Join(cfgAbs, ".codex")
	path := filepath.Join(dir, "config.toml")

	existing, _ := os.ReadFile(path) // missing file → empty
	elems := []string{tomlStr("mcp")}
	for _, r := range repos {
		rAbs, err := filepath.Abs(r)
		if err != nil {
			return false, err
		}
		elems = append(elems, tomlStr("--repo"), tomlStr(rAbs))
	}
	block := fmt.Sprintf("[mcp_servers.orchard]\ncommand = %s\nargs = [%s]\n",
		tomlStr(orchardBin), strings.Join(elems, ", "))
	next, changed := upsertTOMLTable(string(existing), "[mcp_servers.orchard]", block)
	if !changed {
		return false, nil
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(path, []byte(next), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

func upsertTOMLTable(existing, header, block string) (string, bool) {
	block = strings.TrimRight(block, "\n") + "\n"
	start, afterHeader := findTOMLTable(existing, header)
	if start < 0 {
		trimmed := strings.TrimRight(existing, "\n")
		if strings.TrimSpace(trimmed) == "" {
			return block, true
		}
		return trimmed + "\n\n" + block, true
	}
	end := findNextTOMLTable(existing, afterHeader)
	old := existing[start:end]
	if strings.TrimSpace(old) == strings.TrimSpace(block) {
		return existing, false
	}
	return existing[:start] + block + existing[end:], true
}

func findTOMLTable(data, header string) (start, afterHeader int) {
	for pos := 0; pos < len(data); {
		lineEnd := strings.IndexByte(data[pos:], '\n')
		next := len(data)
		if lineEnd >= 0 {
			next = pos + lineEnd + 1
		}
		line := strings.TrimRight(data[pos:next], "\n")
		if strings.TrimSpace(line) == header {
			return pos, next
		}
		pos = next
	}
	return -1, -1
}

func findNextTOMLTable(data string, from int) int {
	for pos := from; pos < len(data); {
		lineEnd := strings.IndexByte(data[pos:], '\n')
		next := len(data)
		if lineEnd >= 0 {
			next = pos + lineEnd + 1
		}
		line := strings.TrimSpace(strings.TrimRight(data[pos:next], "\n"))
		if strings.HasPrefix(line, "[") {
			return pos
		}
		pos = next
	}
	return len(data)
}

// tomlStr renders s as a quoted TOML basic string.
func tomlStr(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
