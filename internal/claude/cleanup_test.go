package claude

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanupPeriodDays(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(dir, "settings.json")

	if _, set := CleanupPeriodDays(); set {
		t.Error("absent settings.json should report unset")
	}
	os.WriteFile(settings, []byte(`{"cleanupPeriodDays":0}`), 0o644)
	if d, set := CleanupPeriodDays(); !set || d != 0 {
		t.Fatalf("cleanupPeriodDays=0 -> (%d, %v)", d, set)
	}
	os.WriteFile(settings, []byte(`{"cleanupPeriodDays":30}`), 0o644)
	if d, set := CleanupPeriodDays(); !set || d != 30 {
		t.Fatalf("cleanupPeriodDays=30 -> (%d, %v)", d, set)
	}
	os.WriteFile(settings, []byte(`{"other":true}`), 0o644)
	if _, set := CleanupPeriodDays(); set {
		t.Error("missing key should report unset")
	}
}
