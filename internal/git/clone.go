package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/prakashkurup/orchard/internal/repo"
)

// Clone clones rawURL into root (one level deep) and returns the new directory
// name. It validates the URL form and the derived name, passes "--" before the
// URL, and restricts the git transport, so a pasted URL cannot be read as a
// flag, run a transport helper, or escape the root.
func Clone(ctx context.Context, rawURL, root string) (string, error) {
	url := NormalizeCloneURL(rawURL)
	name := RepoNameFromURL(url)
	if !allowedCloneURL(url) {
		return name, fmt.Errorf("unsupported or unsafe clone URL (use https/ssh/git or owner/repo)")
	}
	if !safeRepoName(name) {
		return name, fmt.Errorf("cannot derive a safe repo name from that URL")
	}
	base := repo.ExpandPath(root)
	if abs, err := filepath.Abs(base); err == nil {
		base = abs
	}
	dest := filepath.Join(base, name)
	if filepath.Dir(dest) != base {
		return name, fmt.Errorf("refusing to clone outside %s", base)
	}
	if _, err := os.Stat(dest); err == nil {
		return name, fmt.Errorf("%s already exists", name)
	}
	cmd := exec.CommandContext(ctx, "git", "clone", "--", url, dest)
	cmd.Env = append(os.Environ(), "GIT_ALLOW_PROTOCOL=https:http:ssh:git")
	if out, err := cmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return name, fmt.Errorf("%s", msg)
	}
	return name, nil
}

// NormalizeCloneURL accepts a full git URL or an "owner/repo" shorthand.
func NormalizeCloneURL(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return ""
	}
	if strings.Contains(u, "://") || strings.HasPrefix(u, "git@") {
		return u
	}
	// owner/repo shorthand -> GitHub SSH (works for private repos with keys)
	if strings.Count(u, "/") == 1 && !strings.ContainsAny(u, " \t") {
		return "git@github.com:" + strings.TrimSuffix(u, ".git") + ".git"
	}
	return u
}

// RepoNameFromURL extracts the bare repository name from a clone URL.
func RepoNameFromURL(u string) string {
	u = strings.TrimRight(strings.TrimSpace(u), "/")
	u = strings.TrimSuffix(u, ".git")
	if i := strings.LastIndexAny(u, "/:"); i >= 0 {
		u = u[i+1:]
	}
	return strings.TrimSuffix(u, ".git")
}

// allowedCloneURL reports whether u is a clone URL form we will hand to git: an
// https/http/ssh/git scheme URL, or scp-style user@host:path. It rejects
// transport-helper / local forms (anything containing "::", e.g. ext::sh -c …).
func allowedCloneURL(u string) bool {
	if u == "" || strings.Contains(u, "::") {
		return false
	}
	for _, p := range []string{"https://", "http://", "ssh://", "git://"} {
		if strings.HasPrefix(u, p) {
			return len(u) > len(p)
		}
	}
	if at := strings.Index(u, "@"); at > 0 {
		rest := u[at+1:]
		if c := strings.Index(rest, ":"); c > 0 && !strings.Contains(rest[:c], "/") {
			return true
		}
	}
	return false
}

// safeRepoName rejects names that would escape the clone root or are unusable.
func safeRepoName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	return !strings.ContainsAny(name, `/\`)
}
