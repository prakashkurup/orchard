package github

import (
	"context"

	ghapi "github.com/google/go-github/v73/github"
)

// PR is a lightweight view of an open pull request.
type PR struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
}

// RepoStatus is a repo's GitHub state: its open pull requests and the latest CI
// conclusion on the default branch.
type RepoStatus struct {
	OpenPRs int    `json:"open_prs"`
	PRs     []PR   `json:"prs"`      // a capped sample, for the detail view
	CIState string `json:"ci_state"` // "passing" | "failing" | "pending" | "" (none/unknown)
	Err     error  `json:"-"`
}

// HasToken reports whether a GitHub token is available, so callers can skip the
// per-repo status fetch entirely rather than failing once per repo.
func HasToken(ctx context.Context) bool {
	_, err := authToken(ctx)
	return err == nil
}

// RepoStatusFor fetches open PRs and the CI status of ref (the default branch)
// for owner/repo. GitHub-only; needs a token (GITHUB_TOKEN or gh auth token).
func RepoStatusFor(ctx context.Context, owner, repo, ref string) RepoStatus {
	var st RepoStatus
	token, err := authToken(ctx)
	if err != nil {
		st.Err = err
		return st
	}
	client := ghapi.NewClient(nil).WithAuthToken(token)

	prs, _, err := client.PullRequests.List(ctx, owner, repo, &ghapi.PullRequestListOptions{
		State:       "open",
		ListOptions: ghapi.ListOptions{PerPage: 50},
	})
	if err != nil {
		st.Err = err
		return st
	}
	st.OpenPRs = len(prs)
	for i, p := range prs {
		if i >= 5 {
			break
		}
		st.PRs = append(st.PRs, PR{Number: p.GetNumber(), Title: p.GetTitle()})
	}

	if ref != "" {
		runs, _, err := client.Checks.ListCheckRunsForRef(ctx, owner, repo, ref, &ghapi.ListCheckRunsOptions{
			ListOptions: ghapi.ListOptions{PerPage: 100},
		})
		if err == nil && runs != nil {
			st.CIState = aggregateChecks(runs.CheckRuns)
		}
	}
	return st
}

// aggregateChecks rolls up check-run conclusions into one CI state: any failure
// wins, then any still-running, otherwise passing. Empty when there are no runs.
func aggregateChecks(runs []*ghapi.CheckRun) string {
	if len(runs) == 0 {
		return ""
	}
	failing, pending := false, false
	for _, r := range runs {
		if r.GetStatus() != "completed" {
			pending = true
			continue
		}
		switch r.GetConclusion() {
		case "failure", "timed_out", "cancelled", "action_required":
			failing = true
		}
	}
	switch {
	case failing:
		return "failing"
	case pending:
		return "pending"
	default:
		return "passing"
	}
}
