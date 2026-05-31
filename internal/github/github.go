// Package github lists and clones repositories from a GitHub org via the GitHub
// API and the git CLI.
package github

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	ghapi "github.com/google/go-github/v73/github"
)

// Clone result statuses, the allowed values of CloneResult.Status.
const (
	StatusCloned  = "cloned"
	StatusSkipped = "skipped"
	StatusFailed  = "failed"
)

// RemoteRepo is a repository discovered on the remote (e.g. a GitHub org).
type RemoteRepo struct {
	Name     string `json:"name"`
	SSHURL   string `json:"ssh_url"`
	CloneURL string `json:"clone_url"`
	Archived bool   `json:"archived"`
}

// CloneResult is the outcome of cloning one remote repository.
type CloneResult struct {
	Repo   RemoteRepo `json:"repo"`
	Path   string     `json:"path"`
	Status string     `json:"status"`
	Reason string     `json:"reason,omitempty"`
	Error  string     `json:"error,omitempty"`
}

// ListOrgRepos lists an org's repositories, optionally filtered by the match
// regexp and excluding archived repos.
func ListOrgRepos(ctx context.Context, org, match string, includeArchived bool) ([]RemoteRepo, error) {
	if org == "" {
		return nil, errors.New("missing org")
	}

	token, err := authToken(ctx)
	if err != nil {
		return nil, err
	}

	var matcher *regexp.Regexp
	if match != "" {
		matcher, err = regexp.Compile(match)
		if err != nil {
			return nil, err
		}
	}

	client := ghapi.NewClient(nil).WithAuthToken(token)
	opts := &ghapi.RepositoryListByOrgOptions{
		Type:        "all",
		ListOptions: ghapi.ListOptions{PerPage: 100},
	}

	var out []RemoteRepo
	for {
		repos, resp, err := client.Repositories.ListByOrg(ctx, org, opts)
		if err != nil {
			return nil, err
		}
		for _, r := range repos {
			name := r.GetName()
			if !includeArchived && r.GetArchived() {
				continue
			}
			if matcher != nil && !matcher.MatchString(name) {
				continue
			}
			out = append(out, RemoteRepo{
				Name:     name,
				SSHURL:   r.GetSSHURL(),
				CloneURL: r.GetCloneURL(),
				Archived: r.GetArchived(),
			})
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return out, nil
}

// CloneOrg lists an org's repos and clones them into root.
func CloneOrg(ctx context.Context, org, root, match string, includeArchived bool, concurrency int) ([]CloneResult, error) {
	repos, err := ListOrgRepos(ctx, org, match, includeArchived)
	if err != nil {
		return nil, err
	}
	return CloneRepos(ctx, repos, root, concurrency), nil
}

// CloneRepos clones each repo into root concurrently, returning results in order.
func CloneRepos(ctx context.Context, repos []RemoteRepo, root string, concurrency int) []CloneResult {
	if concurrency <= 0 {
		concurrency = 4
	}

	type result struct {
		index int
		item  CloneResult
	}

	results := make(chan result, len(repos))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i, rr := range repos {
		wg.Add(1)
		go func(i int, rr RemoteRepo) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results <- result{index: i, item: CloneResult{Repo: rr, Status: StatusFailed, Error: ctx.Err().Error()}}
				return
			}
			results <- result{index: i, item: cloneOne(ctx, rr, root)}
		}(i, rr)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	out := make([]CloneResult, len(repos))
	for res := range results {
		out[res.index] = res.item
	}
	return out
}

func cloneOne(ctx context.Context, rr RemoteRepo, root string) CloneResult {
	path := filepath.Join(root, rr.Name)
	result := CloneResult{Repo: rr, Path: path}

	if _, err := os.Stat(path); err == nil {
		result.Status = StatusSkipped
		result.Reason = "directory already exists"
		return result
	} else if !os.IsNotExist(err) {
		result.Status = StatusFailed
		result.Error = err.Error()
		return result
	}

	url := rr.SSHURL
	if url == "" {
		url = rr.CloneURL
	}
	if url == "" {
		result.Status = StatusFailed
		result.Error = "repo has no clone URL"
		return result
	}

	// "--" guards against a "-"-prefixed URL; GIT_ALLOW_PROTOCOL blocks ext::/
	// file:: transport helpers even though these URLs come from the GitHub API.
	cmd := exec.CommandContext(ctx, "git", "clone", "--quiet", "--", url, path)
	cmd.Env = append(os.Environ(), "GIT_ALLOW_PROTOCOL=https:http:ssh:git")
	if out, err := cmd.CombinedOutput(); err != nil {
		result.Status = StatusFailed
		result.Error = strings.TrimSpace(string(out))
		if result.Error == "" {
			result.Error = err.Error()
		}
		return result
	}

	result.Status = StatusCloned
	return result
}

func authToken(ctx context.Context) (string, error) {
	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
		return token, nil
	}
	cmd := exec.CommandContext(ctx, "gh", "auth", "token")
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return "", fmt.Errorf("gh auth token: %s", msg)
		}
		return "", fmt.Errorf("gh auth token: %w", err)
	}
	token := strings.TrimSpace(string(out))
	if token == "" {
		return "", errors.New("empty GitHub token")
	}
	return token, nil
}
