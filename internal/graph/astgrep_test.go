package graph

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestASTGrepKotlin proves the ast-grep backend parses the Kotlin constructs the
// pure-Go grammar fails on (object / companion object / suspend) and extracts
// their symbols + a call edge. Skipped when ast-grep isn't installed.
func TestASTGrepKotlin(t *testing.T) {
	ag, ok := newASTGrep()
	if !ok {
		t.Skip("ast-grep not installed; skipping")
	}

	repo := t.TempDir()
	src := `package demo

object Singleton {
    fun hello() = greet()
}

class Greeter {
    suspend fun fetch(): Int {
        return compute()
    }

    companion object {
        fun make() = Greeter()
    }
}

fun greet(): String = "hi"

fun compute(): Int = 1
`
	if err := os.WriteFile(filepath.Join(repo, "demo.kt"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := ag.Extract(context.Background(), repo, "kotlin", []SourceFile{{Rel: "demo.kt", Data: []byte(src)}})
	if err != nil {
		t.Fatal(err)
	}
	res := out["demo.kt"]

	names := map[string]Kind{}
	for _, s := range res.Symbols {
		names[s.Name] = s.Kind
	}
	for _, want := range []string{"Singleton", "Greeter", "fetch", "greet", "compute"} {
		if _, found := names[want]; !found {
			t.Errorf("missing symbol %q (got %v)", want, names)
		}
	}
	if names["Singleton"] != KindClass {
		t.Errorf("object Singleton kind = %q, want %q", names["Singleton"], KindClass)
	}
	if len(res.Edges) == 0 {
		t.Error("expected call edges from ast-grep, got none")
	}
}

func TestNewASTGrepPrefersOverrideThenManagedBinary(t *testing.T) {
	home := t.TempDir()
	xdg := filepath.Join(home, ".config")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)

	pathDir := t.TempDir()
	pathBin := fakeExecutable(t, pathDir, "ast-grep")
	t.Setenv("PATH", pathDir)

	override := fakeExecutable(t, t.TempDir(), "custom-sg")
	t.Setenv(astGrepPathEnv, override)
	ag, ok := newASTGrep()
	if !ok || ag.bin != override {
		t.Fatalf("newASTGrep override = (%q,%v), want %q,true", ag.bin, ok, override)
	}

	t.Setenv(astGrepPathEnv, "")
	dir, err := binDir()
	if err != nil {
		t.Fatal(err)
	}
	managed := fakeExecutable(t, dir, "ast-grep")
	ag, ok = newASTGrep()
	if !ok || ag.bin != managed {
		t.Fatalf("newASTGrep managed = (%q,%v), want %q,true (PATH had %q)", ag.bin, ok, managed, pathBin)
	}
}

func fakeExecutable(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	// answers --version like the real ast-grep, so PATH verification accepts it
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho ast-grep 0.0.0-test\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestNewASTGrepRejectsImpostorSg pins the Linux footgun: /usr/bin/sg is the
// unrelated setgroups utility, so a PATH hit that does not identify itself as
// ast-grep must be rejected rather than silently producing empty graphs.
func TestNewASTGrepRejectsImpostorSg(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv(astGrepPathEnv, "")

	pathDir := t.TempDir()
	impostor := filepath.Join(pathDir, "sg")
	if err := os.WriteFile(impostor, []byte("#!/bin/sh\necho 'usage: sg group command'; exit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", pathDir)

	if ag, ok := newASTGrep(); ok {
		t.Fatalf("newASTGrep accepted an impostor sg: %q", ag.bin)
	}
}
