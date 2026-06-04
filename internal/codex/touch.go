package codex

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/prakashkurup/orchard/internal/agent"
)

// TouchMap scans the newest `limit` Codex sessions for a repo and returns the
// files Codex edited there (from patch_apply_end), scoped to the repo root and
// sorted edits-first. Codex applies changes as patches, so these are writes;
// reads are not recorded in the rollout.
func TouchMap(repoPath string, limit int) []agent.TouchedFile {
	repoPath = filepath.Clean(repoPath)
	prefix := repoPath + string(os.PathSeparator)
	refs := refsFor(repoPath)
	if limit > 0 && len(refs) > limit {
		refs = refs[:limit]
	}
	agg := map[string]*agent.TouchedFile{}
	for _, r := range refs {
		scanPatches(r.path, r.mod, repoPath, prefix, agg)
	}
	out := make([]agent.TouchedFile, 0, len(agg))
	for _, t := range agg {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Wrote() != b.Wrote() {
			return a.Wrote()
		}
		if a.Touches() != b.Touches() {
			return a.Touches() > b.Touches()
		}
		if !a.Last.Equal(b.Last) {
			return a.Last.After(b.Last)
		}
		return a.Path < b.Path
	})
	return out
}

func scanPatches(path string, mod time.Time, repoPath, prefix string, agg map[string]*agent.TouchedFile) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	r := bufio.NewReader(f)
	for {
		line, over, err := readCappedLine(r)
		if !over && strings.Contains(line, `"patch_apply_end"`) {
			var rec struct {
				Type    string `json:"type"`
				Payload struct {
					Type    string                     `json:"type"`
					Success bool                       `json:"success"`
					Changes map[string]json.RawMessage `json:"changes"`
				} `json:"payload"`
			}
			if json.Unmarshal([]byte(line), &rec) == nil &&
				rec.Type == "event_msg" && rec.Payload.Type == "patch_apply_end" && rec.Payload.Success {
				for p := range rec.Payload.Changes {
					rel, ok := repoRel(p, repoPath, prefix)
					if !ok {
						continue
					}
					t := agg[rel]
					if t == nil {
						t = &agent.TouchedFile{Path: rel}
						agg[rel] = t
					}
					t.Writes++
					if mod.After(t.Last) {
						t.Last = mod
					}
				}
			}
		}
		if err != nil {
			break
		}
	}
}

func repoRel(p, repoPath, prefix string) (string, bool) {
	if p == "" {
		return "", false
	}
	if strings.HasPrefix(p, prefix) {
		return p[len(prefix):], true
	}
	if p == repoPath {
		return filepath.Base(repoPath), true
	}
	if !filepath.IsAbs(p) {
		return p, true
	}
	return "", false
}
