package graph

import "testing"

func TestGoASTParse(t *testing.T) {
	src := []byte(`package p

type T struct{}

func (t T) M() { helper() }

func helper() int { return 1 }

const C = 2

var V = 3
`)
	res := parseGo("p.go", src)
	if len(res.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", res.Diagnostics)
	}

	got := map[string]Kind{}
	for _, s := range res.Symbols {
		got[s.Name] = s.Kind
	}
	want := map[string]Kind{"T": KindStruct, "M": KindMethod, "helper": KindFunc, "C": KindConst, "V": KindVar}
	for name, kind := range want {
		if got[name] != kind {
			t.Errorf("symbol %q: got kind %q, want %q", name, got[name], kind)
		}
	}

	var found bool
	for _, e := range res.Edges {
		if e.SrcName == "M" && e.DstName == "helper" && e.Kind == EdgeCall {
			found = true
		}
	}
	if !found {
		t.Errorf("missing call edge M->helper; edges=%+v", res.Edges)
	}
}

func TestGoASTSyntaxErrorIsSoft(t *testing.T) {
	res := parseGo("bad.go", []byte("package p\nfunc ("))
	if len(res.Diagnostics) == 0 {
		t.Error("expected a diagnostic for invalid Go")
	}
}

func TestGoASTTier(t *testing.T) {
	if got := (goASTProvider{}).Tier("go"); got != TierPrecise {
		t.Errorf("Tier(go) = %q, want %q", got, TierPrecise)
	}
	if got := (goASTProvider{}).Tier("python"); got != TierUnsupported {
		t.Errorf("Tier(python) = %q, want %q", got, TierUnsupported)
	}
}
