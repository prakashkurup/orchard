package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadResolvesRelativeRootFromConfigLocation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("root: ..\nconcurrency: 4\norg: test-org\nscope:\n  match: \"^svc-\"\n  repos: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded != path {
		t.Fatalf("loaded path = %q, want %q", loaded, path)
	}
	wantRoot := filepath.Dir(dir)
	if cfg.Root != wantRoot {
		t.Fatalf("root = %q, want %q", cfg.Root, wantRoot)
	}
	if cfg.Concurrency != 4 {
		t.Fatalf("concurrency = %d, want 4", cfg.Concurrency)
	}
	if cfg.Org != "test-org" {
		t.Fatalf("org = %q, want test-org", cfg.Org)
	}
	if cfg.Scope.Match != "^svc-" {
		t.Fatalf("scope match = %q, want ^svc-", cfg.Scope.Match)
	}
}
