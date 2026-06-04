package repo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestComputeDisplay(t *testing.T) {
	cases := []struct {
		name string
		r    Repo
		want DisplayState
	}{
		{"error", Repo{Err: "boom"}, DisplayError},
		{"detached", Repo{Detached: true}, DisplayDetached},
		{"dirty", Repo{Dirty: true, HasUpstream: true}, DisplayDirty},
		{"diverged", Repo{Ahead: 1, Behind: 1, HasUpstream: true}, DisplayDiverged},
		{"behind", Repo{Behind: 2, HasUpstream: true}, DisplayBehind},
		{"ahead", Repo{Ahead: 2, HasUpstream: true}, DisplayAhead},
		{"feature", Repo{Branch: "feat/x", DefaultBranch: "main", OnDefault: false, HasUpstream: true}, DisplayFeature},
		{"no-upstream", Repo{Branch: "main", DefaultBranch: "main", OnDefault: true, HasUpstream: false}, DisplayNoUpstream},
		{"clean", Repo{Branch: "main", DefaultBranch: "main", OnDefault: true, HasUpstream: true}, DisplayClean},
	}
	for _, c := range cases {
		if got := ComputeDisplay(c.r); got != c.want {
			t.Errorf("%s: ComputeDisplay = %s, want %s", c.name, got, c.want)
		}
	}
}

func TestDisplayStateStringAndGlyph(t *testing.T) {
	states := []DisplayState{
		DisplayClean, DisplayDirty, DisplayBehind, DisplayAhead, DisplayDiverged,
		DisplayFeature, DisplayDetached, DisplayNoUpstream, DisplayError,
	}
	for _, s := range states {
		if s.String() == "" || s.String() == "unknown" {
			t.Errorf("missing String() for state %d", s)
		}
		if s.Glyph() == "" {
			t.Errorf("missing Glyph() for state %d", s)
		}
	}
}

func TestWithDisplayDerivesOnDefault(t *testing.T) {
	r := Repo{Branch: "main", DefaultBranch: "main", HasUpstream: true}.WithDisplay()
	if !r.OnDefault {
		t.Error("WithDisplay should set OnDefault when branch == default")
	}
	if r.Display != DisplayClean {
		t.Errorf("display = %s, want clean", r.Display)
	}
}

func TestPullSkipReason(t *testing.T) {
	cases := []struct {
		r       Repo
		wantSub string
	}{
		{Repo{Dirty: true}, "dirty"},
		{Repo{Detached: true}, "detached"},
		{Repo{HasUpstream: false}, "no upstream"},
		{Repo{Branch: "main", DefaultBranch: "main", OnDefault: true, HasUpstream: true}, ""},
		// A trunk that is not the repo's recorded default (e.g. "main" while the
		// remote default is still "master") is still pullable: pull is ff-only.
		{Repo{Branch: "main", DefaultBranch: "master", OnDefault: false, HasUpstream: true}, ""},
		// ...but a non-default branch with no upstream has nothing to pull from.
		{Repo{Branch: "feat", DefaultBranch: "main", OnDefault: false, HasUpstream: false}, "no upstream"},
	}
	for _, c := range cases {
		got := c.r.PullSkipReason()
		if c.wantSub == "" {
			if got != "" {
				t.Errorf("expected pullable, got reason %q", got)
			}
			continue
		}
		if !contains(got, c.wantSub) {
			t.Errorf("PullSkipReason = %q, want substring %q", got, c.wantSub)
		}
	}
}

func TestExpandPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	if got := ExpandPath("~/x"); got != filepath.Join(home, "x") {
		t.Errorf("ExpandPath(~/x) = %q", got)
	}
	if got := ExpandPath("~"); got != home {
		t.Errorf("ExpandPath(~) = %q", got)
	}
	if got := ExpandPath("/abs/path"); got != "/abs/path" {
		t.Errorf("ExpandPath(/abs/path) = %q", got)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
