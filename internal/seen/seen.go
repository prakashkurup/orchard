// Package seen persists the HEAD sha of each repo as of the last time orchard
// was opened, so the dashboard can highlight what changed "since last visit".
package seen

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func statePath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "orchard", "seen.json")
}

// Load returns the saved map of repo path -> last-seen HEAD sha.
func Load() map[string]string {
	out := map[string]string{}
	p := statePath()
	if p == "" {
		return out
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return out
	}
	_ = json.Unmarshal(data, &out)
	return out
}

// Save persists the current map of repo path -> HEAD sha.
func Save(m map[string]string) error {
	p := statePath()
	if p == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}
