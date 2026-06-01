package github

import (
	"testing"

	ghapi "github.com/google/go-github/v73/github"
)

func TestAggregateChecks(t *testing.T) {
	run := func(status, concl string) *ghapi.CheckRun {
		r := &ghapi.CheckRun{Status: ghapi.Ptr(status)}
		if concl != "" {
			r.Conclusion = ghapi.Ptr(concl)
		}
		return r
	}
	cases := []struct {
		name string
		runs []*ghapi.CheckRun
		want string
	}{
		{"none", nil, ""},
		{"all pass", []*ghapi.CheckRun{run("completed", "success"), run("completed", "success")}, "passing"},
		{"one fail", []*ghapi.CheckRun{run("completed", "success"), run("completed", "failure")}, "failing"},
		{"running", []*ghapi.CheckRun{run("completed", "success"), run("in_progress", "")}, "pending"},
		{"fail beats running", []*ghapi.CheckRun{run("in_progress", ""), run("completed", "failure")}, "failing"},
	}
	for _, c := range cases {
		if got := aggregateChecks(c.runs); got != c.want {
			t.Errorf("%s: aggregateChecks = %q, want %q", c.name, got, c.want)
		}
	}
}
