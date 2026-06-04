package tui

import (
	"testing"
	"time"

	"github.com/prakashkurup/orchard/internal/repo"
)

func TestFetchIntervalSecs(t *testing.T) {
	if got := fetchIntervalSecs(); got != 300 {
		t.Errorf("default = %d, want 300", got)
	}
	t.Setenv("ORCHARD_FETCH_SECS", "60")
	if got := fetchIntervalSecs(); got != 60 {
		t.Errorf("override = %d, want 60", got)
	}
	t.Setenv("ORCHARD_FETCH_SECS", "0")
	if fetchIntervalSecs() != 0 {
		t.Error("0 should disable background fetch")
	}
	if fetchTickCmd() != nil {
		t.Error("fetchTickCmd should be nil when disabled")
	}
}

func TestBackgroundFetchGating(t *testing.T) {
	base := func() model {
		m := newModel("root", 4)
		m.mode = modeList
		m.loading = false // scan has finished
		m.repos = []repo.Repo{{Path: "/a", Name: "a"}}
		return m
	}

	// live + idle + has repos -> a background fetch starts
	m := base()
	m.autoRefresh = true
	mm, _ := m.Update(fetchTickMsg(time.Now()))
	if !mm.(model).bgFetching {
		t.Fatal("expected a background fetch to start while live")
	}

	// live off -> no background fetch
	m = base()
	m.autoRefresh = false
	mm, _ = m.Update(fetchTickMsg(time.Now()))
	if mm.(model).bgFetching {
		t.Error("no background fetch when live is off")
	}

	// no repos -> nothing to fetch
	m = base()
	m.autoRefresh = true
	m.repos = nil
	mm, _ = m.Update(fetchTickMsg(time.Now()))
	if mm.(model).bgFetching {
		t.Error("no background fetch when there are no repos")
	}

	// completion clears the flag and applies the rescanned repos
	m = base()
	m.bgFetching = true
	mm, _ = m.Update(bgFetchMsg{repos: []repo.Repo{{Path: "/a"}, {Path: "/b"}}})
	if mm.(model).bgFetching {
		t.Error("bgFetchMsg should clear bgFetching")
	}
	if len(mm.(model).repos) != 2 {
		t.Errorf("bgFetchMsg should apply the rescanned repos, got %d", len(mm.(model).repos))
	}
}
