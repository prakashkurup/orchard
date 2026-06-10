package graph

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
)

// astGrepVersion is the pinned ast-grep release. Pinning keeps the per-language
// rules stable (its node kinds / pattern behaviour can shift across versions).
// To bump: change this and the SHA-256s below (the release publishes no
// checksums file, so we hardcode the asset digests).
const astGrepVersion = "0.43.0"

type agAsset struct{ file, sha256 string }

// astGrepAssets maps GOOS/GOARCH to the release zip and its verified SHA-256.
var astGrepAssets = map[string]agAsset{
	"darwin/arm64": {"app-aarch64-apple-darwin.zip", "8c847d0a29aa4b3101b3361e0b3ee7fb53c7e497adc9ed1afc9615538cd40782"},
	"darwin/amd64": {"app-x86_64-apple-darwin.zip", "6d703090b106747b2f56086b6ccc7e798fe78bcae70257aa20519b220153555b"},
	"linux/arm64":  {"app-aarch64-unknown-linux-gnu.zip", "e706846148493967f3ab8011334817edd86ce5acbec10718b2a7b40799c640ff"},
	"linux/amd64":  {"app-x86_64-unknown-linux-gnu.zip", "a26253a9c821d935f7e383e40f0de7c2ca62a4121de1f73a6d81ec32eae631e0"},
}

// InstallASTGrep downloads the pinned ast-grep for the current platform into
// <user-config>/orchard/bin (verifying its SHA-256) and returns the binary path,
// so non-Go languages work without the user installing ast-grep separately.
// Downloading via Go's HTTP client (not a browser) means no macOS quarantine
// xattr is set, so the binary runs without a Gatekeeper prompt.
func InstallASTGrep(ctx context.Context) (string, error) {
	key := runtime.GOOS + "/" + runtime.GOARCH
	a, ok := astGrepAssets[key]
	if !ok {
		return "", fmt.Errorf("orchard: no pinned ast-grep %s build for %s — install it manually (e.g. brew install ast-grep)", astGrepVersion, key)
	}
	dir, err := binDir()
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("https://github.com/ast-grep/ast-grep/releases/download/%s/%s", astGrepVersion, a.file)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if got := hex.EncodeToString(sha256Sum(data)); got != a.sha256 {
		return "", fmt.Errorf("ast-grep checksum mismatch for %s: got %s, want %s", a.file, got, a.sha256)
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	var astGrepPath string
	for _, f := range zr.File {
		base := filepath.Base(f.Name)
		if base != "ast-grep" && base != "sg" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		b, rerr := io.ReadAll(rc)
		rc.Close()
		if rerr != nil {
			return "", rerr
		}
		out := filepath.Join(dir, base)
		if err := os.WriteFile(out, b, 0o755); err != nil {
			return "", err
		}
		if base == "ast-grep" {
			astGrepPath = out
		}
	}
	if astGrepPath == "" {
		return "", fmt.Errorf("ast-grep binary not found inside %s", a.file)
	}
	return astGrepPath, nil
}

func sha256Sum(b []byte) []byte {
	s := sha256.Sum256(b)
	return s[:]
}
