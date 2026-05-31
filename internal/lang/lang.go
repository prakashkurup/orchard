// Package lang detects a repo's dominant programming language by counting
// tracked-file extensions (via git, already required) - no external tools. It
// maps extensions to language names and their GitHub brand colors.
package lang

import (
	"context"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Stat is one language and its share.
type Stat struct {
	Name  string
	Color string
	Icon  string // Nerd Font devicon ("" if none)
	Pct   int
}

type langDef struct {
	name  string
	color string
	icon  string
}

// extLang maps lower-case extensions to a language, GitHub color, and Nerd Font
// devicon. Icons are from the Nerd Font dev-/seti- range.
var extLang = map[string]langDef{
	"go":    {"Go", "#00ADD8", ""},
	"kt":    {"Kotlin", "#A97BFF", ""},
	"kts":   {"Kotlin", "#A97BFF", ""},
	"java":  {"Java", "#B07219", ""},
	"ts":    {"TypeScript", "#3178C6", ""},
	"tsx":   {"TypeScript", "#3178C6", ""},
	"js":    {"JavaScript", "#F1E05A", ""},
	"jsx":   {"JavaScript", "#F1E05A", ""},
	"mjs":   {"JavaScript", "#F1E05A", ""},
	"py":    {"Python", "#3572A5", ""},
	"rb":    {"Ruby", "#701516", ""},
	"rs":    {"Rust", "#DEA584", ""},
	"swift": {"Swift", "#F05138", ""},
	"scala": {"Scala", "#C22D40", ""},
	"php":   {"PHP", "#4F5D95", ""},
	"c":     {"C", "#555555", ""},
	"h":     {"C", "#555555", ""},
	"cc":    {"C++", "#F34B7D", ""},
	"cpp":   {"C++", "#F34B7D", ""},
	"hpp":   {"C++", "#F34B7D", ""},
	"cs":    {"C#", "#178600", ""},
	"sh":    {"Shell", "#89E051", ""},
	"bash":  {"Shell", "#89E051", ""},
	"zsh":   {"Shell", "#89E051", ""},
	"html":  {"HTML", "#E34C26", ""},
	"css":   {"CSS", "#563D7C", ""},
	"scss":  {"SCSS", "#C6538C", ""},
	"vue":   {"Vue", "#41B883", ""},
	"dart":  {"Dart", "#00B4AB", ""},
	"ex":    {"Elixir", "#6E4A7E", ""},
	"exs":   {"Elixir", "#6E4A7E", ""},
	"clj":   {"Clojure", "#DB5855", ""},
	"lua":   {"Lua", "#000080", ""},
	"sql":   {"SQL", "#E38C00", ""},
	"proto": {"Protobuf", "#C95B47", ""},
	"md":    {"Markdown", "#083FA1", ""},
	"json":  {"JSON", "#292929", ""},
	"yaml":  {"YAML", "#CB171E", ""},
	"yml":   {"YAML", "#CB171E", ""},
}

// docLangs are docs/config/data languages - counted, but never chosen as a
// repo's dominant language when any real code language is present.
var docLangs = map[string]bool{
	"Markdown": true, "JSON": true, "YAML": true, "Text": true,
}

// Detect returns languages sorted by share, dominant first. Empty if unknown.
func Detect(ctx context.Context, repoPath string) []Stat {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "ls-files", "-z")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	counts := map[string]int{}
	defs := map[string]langDef{}
	total := 0
	for _, f := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if f == "" {
			continue
		}
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(f), "."))
		if d, ok := extLang[ext]; ok {
			counts[d.name]++
			defs[d.name] = d
			total++
		}
	}
	if total == 0 {
		return nil
	}
	stats := make([]Stat, 0, len(counts))
	for name, c := range counts {
		d := defs[name]
		stats = append(stats, Stat{Name: name, Color: d.color, Icon: d.icon, Pct: c * 100 / total})
	}
	sort.Slice(stats, func(i, j int) bool {
		di, dj := docLangs[stats[i].Name], docLangs[stats[j].Name]
		if di != dj {
			return !di // real code languages before docs/data
		}
		if stats[i].Pct != stats[j].Pct {
			return stats[i].Pct > stats[j].Pct
		}
		return stats[i].Name < stats[j].Name
	})
	return stats
}

// Dominant returns the top language, or a zero Stat.
func Dominant(ctx context.Context, repoPath string) Stat {
	if s := Detect(ctx, repoPath); len(s) > 0 {
		return s[0]
	}
	return Stat{}
}

// ByExtension returns the language Stat for a file extension (with or without a
// leading dot), e.g. "go" or ".ts". ok is false for unknown extensions.
func ByExtension(ext string) (Stat, bool) {
	d, found := extLang[strings.ToLower(strings.TrimPrefix(ext, "."))]
	if !found {
		return Stat{}, false
	}
	return Stat{Name: d.name, Color: d.color, Icon: d.icon}, true
}
