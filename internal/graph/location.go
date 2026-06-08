package graph

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

// dbDir is where per-repo graph databases live: <user-config>/orchard/graph.
func dbDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "orchard", "graph"), nil
}

// binDir is where orchard keeps helper binaries (e.g. a downloaded ast-grep):
// <user-config>/orchard/bin.
func binDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "orchard", "bin"), nil
}

// encodeRepo turns a repo's absolute path into a stable, collision-resistant
// flat filename. Keep the basename for human debuggability and hash the full
// path so paths like /a.b/c and /a/b.c never share a graph DB.
func encodeRepo(repoAbs string) string {
	clean := filepath.Clean(repoAbs)
	base := filepath.Base(clean)
	if base == "." || base == string(filepath.Separator) || base == "" {
		base = "repo"
	}
	sum := sha256.Sum256([]byte(clean))
	return base + "-" + hex.EncodeToString(sum[:8])
}

// DBPath returns the graph database path for a repo's absolute path.
func DBPath(repoAbs string) (string, error) {
	dir, err := dbDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, encodeRepo(repoAbs)+".db"), nil
}

// OpenForRepo opens (creating dirs/DB as needed) the graph for a repo's absolute
// path, under orchard's config directory.
func OpenForRepo(repoAbs string) (*Graph, error) {
	dir, err := dbDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return Open(filepath.Join(dir, encodeRepo(repoAbs)+".db"))
}
