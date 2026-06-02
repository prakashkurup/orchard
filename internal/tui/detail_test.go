package tui

import "testing"

func TestDirtyPathSet(t *testing.T) {
	lines := []string{
		" M src/a.go",
		"?? new.txt",
		"R  old.go -> new.go",       // staged rename: both sides count as dirty
		" M " + `"with space.txt"`,  // quoted, no escape
		" M " + `"tab\tx.txt"`,      // C-escaped tab
		" M " + `"utf\303\251.txt"`, // C-escaped UTF-8 (é)
	}
	got := dirtyPathSet(lines)
	for _, want := range []string{
		"src/a.go", "new.txt", "old.go", "new.go",
		"with space.txt", "tab\tx.txt", "utfé.txt",
	} {
		if !got[want] {
			t.Errorf("dirtyPathSet missing %q\nset=%v", want, got)
		}
	}
}
