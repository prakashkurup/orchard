// Package config loads and resolves orchard's configuration (repo root, org,
// concurrency, scope) from a config file, environment, and built-in defaults.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/prakashkurup/orchard/internal/git"
	"github.com/prakashkurup/orchard/internal/repo"
)

// Config is orchard's resolved configuration.
type Config struct {
	Root        string
	Org         string
	Concurrency int
	Scope       Scope
}

// Scope narrows which repos an operation applies to.
type Scope struct {
	Match string
	Repos []string
}

// Default returns the configuration from environment and built-in defaults.
func Default() Config {
	root := os.Getenv("ORCHARD_ROOT")
	if root == "" {
		if cwd, err := os.Getwd(); err == nil {
			root = cwd
		} else {
			root = "."
		}
	}
	return Config{
		Root:        repo.ExpandPath(root),
		Concurrency: git.DefaultConcurrency,
	}
}

// Load reads the config file (searching upward when path is empty), layered
// over Default, and returns the resolved Config and the file path it used.
func Load(path string) (Config, string, error) {
	cfg := Default()
	resolved, exists, err := ResolvePath(path)
	if err != nil {
		return cfg, "", err
	}
	if !exists {
		return cfg, "", nil
	}
	if err := applyFile(&cfg, resolved); err != nil {
		return cfg, resolved, err
	}
	cfg.Root = repo.ExpandPath(cfg.Root)
	if cfg.Root != "" && !filepath.IsAbs(cfg.Root) {
		cfg.Root = filepath.Join(filepath.Dir(resolved), cfg.Root)
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = git.DefaultConcurrency
	}
	return cfg, resolved, nil
}

// ResolvePath cleans and validates a repo-root path, reporting whether it exists.
func ResolvePath(path string) (string, bool, error) {
	if path != "" {
		resolved := repo.ExpandPath(path)
		_, err := os.Stat(resolved)
		if err != nil {
			return resolved, false, err
		}
		return resolved, true, nil
	}

	if envPath := os.Getenv("ORCHARD_CONFIG"); envPath != "" {
		resolved := repo.ExpandPath(envPath)
		_, err := os.Stat(resolved)
		if err != nil {
			return resolved, false, err
		}
		return resolved, true, nil
	}

	candidates := []string{"config.yaml"}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "config.yaml"))
	}
	if dir, err := os.UserConfigDir(); err == nil {
		candidates = append(candidates, filepath.Join(dir, "orchard", "config.yaml"))
	}

	for _, candidate := range candidates {
		resolved := repo.ExpandPath(candidate)
		if _, err := os.Stat(resolved); err == nil {
			return resolved, true, nil
		} else if !os.IsNotExist(err) {
			return resolved, false, err
		}
	}
	return "", false, nil
}

func applyFile(cfg *Config, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	section := ""
	scanner := bufio.NewScanner(file)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		raw := scanner.Text()
		line := stripComment(raw)
		if strings.TrimSpace(line) == "" {
			continue
		}

		indent := len(line) - len(strings.TrimLeft(line, " "))
		trimmed := strings.TrimSpace(line)
		if !strings.Contains(trimmed, ":") {
			return fmt.Errorf("%s:%d: expected key: value", path, lineNo)
		}

		key, value, _ := strings.Cut(trimmed, ":")
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)

		if value == "" && indent == 0 {
			section = key
			continue
		}
		if indent == 0 {
			section = ""
		}

		switch {
		case section == "scope" && key == "match":
			cfg.Scope.Match = value
		case section == "scope" && key == "repos":
			cfg.Scope.Repos = parseList(value)
		case section == "":
			if err := applyTopLevel(cfg, key, value); err != nil {
				return fmt.Errorf("%s:%d: %w", path, lineNo, err)
			}
		default:
			return fmt.Errorf("%s:%d: unknown key %q in section %q", path, lineNo, key, section)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func applyTopLevel(cfg *Config, key, value string) error {
	switch key {
	case "root":
		cfg.Root = value
	case "org":
		cfg.Org = value
	case "concurrency":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid concurrency %q", value)
		}
		cfg.Concurrency = n
	default:
		return fmt.Errorf("unknown key %q", key)
	}
	return nil
}

func stripComment(line string) string {
	if i := strings.Index(line, "#"); i >= 0 {
		return line[:i]
	}
	return line
}

func parseList(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" || value == "[]" {
		return nil
	}
	value = strings.TrimPrefix(value, "[")
	value = strings.TrimSuffix(value, "]")
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(strings.TrimSpace(part), `"'`)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
