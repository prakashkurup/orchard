// Package update checks GitHub Releases for a newer orchard and can replace the
// running binary in place. The version check is a single unauthenticated GET,
// cached for a day, opt out with ORCHARD_NO_UPDATE_CHECK.
package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

const repoSlug = "prakashkurup/orchard"

const checkInterval = 24 * time.Hour

// Current resolves the running version: the ldflag-injected version if present,
// else the module version recorded by `go install` (build info), else "dev".
func Current(ldflag string) string {
	if ldflag != "" && ldflag != "dev" {
		return ldflag
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if v := bi.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	if ldflag != "" {
		return ldflag
	}
	return "dev"
}

// ErrAlreadyLatest is returned by Apply when the running version is current.
var ErrAlreadyLatest = errors.New("already on the latest version")

type asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

type release struct {
	Tag    string  `json:"tag_name"`
	Assets []asset `json:"assets"`
}

// Check returns the latest tag and whether it is newer than current, using a
// 24h on-disk cache. Honors ORCHARD_NO_UPDATE_CHECK. Network errors are soft.
func Check(ctx context.Context, current string) (tag string, available bool) {
	if disabled() {
		return "", false
	}
	if cp := cachePath(); cp != "" {
		if data, err := os.ReadFile(cp); err == nil {
			var c struct {
				Tag       string `json:"tag"`
				CheckedAt int64  `json:"checked_at"`
			}
			if json.Unmarshal(data, &c) == nil && time.Since(time.Unix(c.CheckedAt, 0)) < checkInterval {
				return c.Tag, Newer(current, c.Tag)
			}
		}
	}
	rel, err := latest(ctx)
	if err != nil || rel.Tag == "" {
		return "", false
	}
	writeCache(rel.Tag)
	return rel.Tag, Newer(current, rel.Tag)
}

// Apply downloads the latest release for this OS/arch, verifies its checksum, and
// replaces the running binary. It refuses (with guidance) for go-install and
// Homebrew builds, which have their own update path.
func Apply(ctx context.Context, current string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if hint := managedHint(exe); hint != "" {
		return "", errors.New(hint)
	}
	rel, err := latest(ctx)
	if err != nil {
		return "", err
	}
	if !Newer(current, rel.Tag) {
		return rel.Tag, ErrAlreadyLatest
	}

	suffix := fmt.Sprintf("_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	var tarURL, sumURL string
	for _, a := range rel.Assets {
		if strings.HasSuffix(a.Name, suffix) {
			tarURL = a.URL
		}
		if a.Name == "checksums.txt" {
			sumURL = a.URL
		}
	}
	if tarURL == "" {
		return "", fmt.Errorf("no release asset for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	tgz, err := download(ctx, tarURL)
	if err != nil {
		return "", err
	}
	if sumURL != "" {
		if sums, err := download(ctx, sumURL); err == nil {
			if err := verify(tgz, baseName(tarURL), string(sums)); err != nil {
				return "", err
			}
		}
	}
	bin, err := extractBinary(tgz)
	if err != nil {
		return "", err
	}
	if err := replaceExe(exe, bin); err != nil {
		return "", err
	}
	return rel.Tag, nil
}

// Newer reports whether latest is a strictly higher MAJOR.MINOR.PATCH than
// current. Returns false when current is not a clean version (e.g. "dev"), so
// development builds are never nagged.
func Newer(current, latest string) bool {
	c, ok := parseSemver(current)
	if !ok {
		return false
	}
	l, ok := parseSemver(latest)
	if !ok {
		return false
	}
	for i := 0; i < 3; i++ {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

func parseSemver(s string) ([3]int, bool) {
	var v [3]int
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	s = strings.SplitN(s, "-", 2)[0]
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return v, false
	}
	for i := 0; i < 3; i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

func latest(ctx context.Context) (release, error) {
	var rel release
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.github.com/repos/"+repoSlug+"/releases/latest", nil)
	if err != nil {
		return rel, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return rel, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return rel, fmt.Errorf("github releases: %s", resp.Status)
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel)
	return rel, err
}

func download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download: %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 100<<20))
}

// verify checks the archive's sha256 against the matching line in checksums.txt.
// A missing entry is not fatal (skips), a present-but-mismatched one is.
func verify(data []byte, name, checksums string) error {
	want := ""
	for _, ln := range strings.Split(checksums, "\n") {
		if f := strings.Fields(ln); len(f) == 2 && f[1] == name {
			want = f[0]
		}
	}
	if want == "" {
		return nil
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != want {
		return fmt.Errorf("checksum mismatch for %s", name)
	}
	return nil
}

func extractBinary(tgz []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(tgz))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag == tar.TypeReg && filepath.Base(h.Name) == "orchard" {
			return io.ReadAll(io.LimitReader(tr, 200<<20))
		}
	}
	return nil, errors.New("orchard binary not found in archive")
}

// replaceExe writes the new binary to a temp file in the same directory and
// atomically renames it over the running executable.
func replaceExe(exe string, bin []byte) error {
	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".orchard-update-*")
	if err != nil {
		return fmt.Errorf("cannot write to %s (you may need sudo): %w", dir, err)
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(bin); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, 0o755); err != nil {
		return err
	}
	if err := os.Rename(name, exe); err != nil {
		return fmt.Errorf("cannot replace %s (you may need sudo): %w", exe, err)
	}
	return nil
}

func managedHint(exe string) string {
	goInstall := "orchard looks go-installed; update with: go install github.com/" + repoSlug + "@latest"
	if strings.Contains(exe, "/Cellar/") || strings.Contains(exe, "/homebrew/") {
		return "orchard looks Homebrew-installed; update with: brew upgrade orchard"
	}
	if gobin := os.Getenv("GOBIN"); gobin != "" && strings.HasPrefix(exe, filepath.Clean(gobin)+string(os.PathSeparator)) {
		return goInstall
	}
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		if home, err := os.UserHomeDir(); err == nil {
			gopath = filepath.Join(home, "go")
		}
	}
	if gopath != "" && strings.HasPrefix(exe, filepath.Join(gopath, "bin")+string(os.PathSeparator)) {
		return goInstall
	}
	return ""
}

func disabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ORCHARD_NO_UPDATE_CHECK"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func cachePath() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "orchard", "update.json")
}

func writeCache(tag string) {
	cp := cachePath()
	if cp == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(cp), 0o755)
	data, err := json.Marshal(struct {
		Tag       string `json:"tag"`
		CheckedAt int64  `json:"checked_at"`
	}{tag, time.Now().Unix()})
	if err == nil {
		_ = os.WriteFile(cp, data, 0o644)
	}
}

func baseName(url string) string {
	if i := strings.LastIndexByte(url, '/'); i >= 0 {
		return url[i+1:]
	}
	return url
}
