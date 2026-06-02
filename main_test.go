package main

import (
	"testing"

	"github.com/prakashkurup/orchard/internal/config"
)

// A bare `orchard --root PATH` (no subcommand) must parse the flag, not treat
// "--root" as an unknown command. Regression for that bug.
func TestTUIFlags(t *testing.T) {
	cfg := config.Config{Root: "/default", Concurrency: 8}
	cases := []struct {
		name string
		args []string
		root string
		conc int
	}{
		{"defaults", nil, "/default", 8},
		{"root space", []string{"--root", "/x"}, "/x", 8},
		{"root equals", []string{"--root=/y"}, "/y", 8},
		{"single dash", []string{"-root", "/d"}, "/d", 8},
		{"root and concurrency", []string{"--root", "/z", "--concurrency", "3"}, "/z", 3},
	}
	for _, c := range cases {
		r, n, err := tuiFlags(c.args, cfg)
		if err != nil {
			t.Fatalf("%s: unexpected error %v", c.name, err)
		}
		if r != c.root || n != c.conc {
			t.Errorf("%s: got (%q, %d), want (%q, %d)", c.name, r, n, c.root, c.conc)
		}
	}
	if _, _, err := tuiFlags([]string{"--bogus"}, cfg); err == nil {
		t.Error("an unknown flag should return an error")
	}
}
