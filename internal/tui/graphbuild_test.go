package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/prakashkurup/orchard/internal/repo"
)

func TestBuildQueueLabelShowsLastRepoStats(t *testing.T) {
	targets := []repo.Repo{{Name: "api"}, {Name: "worker"}}
	acc := buildAccum{last: buildResult{name: "api", files: 12, symbols: 34}}

	got := buildQueueLabel(targets, 1, acc)
	for _, want := range []string{
		"tending orchard",
		"api: 12 files / 34 symbols",
		"next 2/2 · worker",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("build queue label missing %q: %s", want, got)
		}
	}
}

func TestBuildSummaryGroupsFailuresAndRetryHint(t *testing.T) {
	acc := buildAccum{
		built:   1,
		files:   12,
		symbols: 34,
		edges:   56,
		failures: []buildFailure{{
			name: "worker",
			err:  errors.New("boom"),
		}},
	}

	got := buildSummary(acc)
	for _, want := range []string{
		"tended 1/2 graph",
		"12 files / 34 symbols",
		"failed: worker",
		"retry failed with B",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("build summary missing %q: %s", want, got)
		}
	}
}
