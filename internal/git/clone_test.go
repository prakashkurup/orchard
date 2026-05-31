package git

import "testing"

func TestNormalizeCloneURL(t *testing.T) {
	cases := map[string]string{
		"acme/foo":                   "git@github.com:acme/foo.git",
		"acme/foo.git":               "git@github.com:acme/foo.git",
		"git@github.com:org/bar.git": "git@github.com:org/bar.git",
		"https://github.com/x/baz":   "https://github.com/x/baz",
		"":                           "",
	}
	for in, want := range cases {
		if got := NormalizeCloneURL(in); got != want {
			t.Errorf("NormalizeCloneURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRepoNameFromURL(t *testing.T) {
	cases := map[string]string{
		"git@github.com:org/bar.git": "bar",
		"https://github.com/x/baz":   "baz",
		"acme/foo":                   "foo",
		"https://h/a/b/c.git/":       "c",
	}
	for in, want := range cases {
		if got := RepoNameFromURL(in); got != want {
			t.Errorf("RepoNameFromURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAllowedCloneURL(t *testing.T) {
	for _, u := range []string{
		"https://github.com/o/r.git", "http://x/y", "ssh://git@h/o/r",
		"git://h/o/r", "git@github.com:o/r.git",
	} {
		if !allowedCloneURL(u) {
			t.Errorf("allowedCloneURL(%q) = false, want true", u)
		}
	}
	for _, u := range []string{
		"", "ext::sh -c id", "--upload-pack=x", "-o", "file:///tmp/x",
		"/local/path", "transport::cmd", "git@:", "https://",
	} {
		if allowedCloneURL(u) {
			t.Errorf("allowedCloneURL(%q) = true, want false (unsafe URL)", u)
		}
	}
}

func TestSafeRepoName(t *testing.T) {
	for _, n := range []string{"repo", "my-repo", "a.b"} {
		if !safeRepoName(n) {
			t.Errorf("safeRepoName(%q) = false, want true", n)
		}
	}
	for _, n := range []string{"", ".", "..", "a/b", `a\b`, "../x"} {
		if safeRepoName(n) {
			t.Errorf("safeRepoName(%q) = true, want false (path escape)", n)
		}
	}
}
