package editor

import "testing"

func TestCatalogAndByID(t *testing.T) {
	if len(Catalog()) == 0 {
		t.Fatal("catalog is empty")
	}
	e, ok := ByID("vscode")
	if !ok || e.Name != "VS Code" {
		t.Fatalf("ByID(vscode) = %+v, ok=%v", e, ok)
	}
	if _, ok := ByID("nope"); ok {
		t.Fatal("ByID(nope) should be false")
	}
}

func TestTerminalEditorsFlagged(t *testing.T) {
	for _, id := range []string{"vim", "nvim"} {
		e, _ := ByID(id)
		if !e.Terminal {
			t.Errorf("%s should be a terminal editor", id)
		}
	}
	e, _ := ByID("vscode")
	if e.Terminal {
		t.Error("vscode should not be a terminal editor")
	}
}

func TestDefaultRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if DefaultID() != "" {
		t.Fatal("expected no default initially")
	}
	if err := SaveDefault("nvim"); err != nil {
		t.Fatal(err)
	}
	if got := DefaultID(); got != "nvim" {
		t.Fatalf("DefaultID = %q, want nvim", got)
	}
}
