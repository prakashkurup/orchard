package seen

import (
	"path/filepath"
	"testing"
)

// isolateState points seen's state file at a fresh temp dir on every OS:
// os.UserConfigDir() reads $XDG_CONFIG_HOME on Linux and $HOME on macOS, so we
// set both. Without the XDG override a pre-set $XDG_CONFIG_HOME (as on CI) is
// shared across tests and leaks state between them.
func isolateState(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
}

func TestSaveLoadRoundTrip(t *testing.T) {
	isolateState(t)
	want := map[string]string{"/a": "sha1", "/b": "sha2"}
	if err := Save(want); err != nil {
		t.Fatal(err)
	}
	got := Load()
	if len(got) != len(want) || got["/a"] != "sha1" || got["/b"] != "sha2" {
		t.Fatalf("Load = %v, want %v", got, want)
	}
}

func TestLoadMissingIsEmpty(t *testing.T) {
	isolateState(t)
	if got := Load(); len(got) != 0 {
		t.Fatalf("Load on missing file = %v, want empty", got)
	}
}
