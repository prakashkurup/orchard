package graph

import "testing"

func TestEncodeRepoIsCollisionResistant(t *testing.T) {
	a := encodeRepo("/tmp/a.b/c")
	b := encodeRepo("/tmp/a/b.c")
	if a == b {
		t.Fatalf("encodeRepo collision: %q == %q", a, b)
	}
}
