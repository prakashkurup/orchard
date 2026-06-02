package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/prakashkurup/orchard/internal/claude"
	"github.com/prakashkurup/orchard/internal/config"
	orchardgit "github.com/prakashkurup/orchard/internal/git"
	orchardgithub "github.com/prakashkurup/orchard/internal/github"
	"github.com/prakashkurup/orchard/internal/lang"
	"github.com/prakashkurup/orchard/internal/repo"
	"github.com/prakashkurup/orchard/internal/tui"
	"github.com/prakashkurup/orchard/internal/update"
)

// version is the build version, overridden at release time via
// -ldflags "-X main.version=...". It is "dev" for local builds.
var version = "dev"

//   quiet rows of trees,
//   each branch waiting to bear fruit;
//   tend them, then let go.

func main() {
	if os.Getenv("NO_COLOR") != "" {
		lipgloss.SetColorProfile(termenv.Ascii)
	}
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "orchard:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	// version is handled before config so it always works, even without a config.
	for _, a := range args {
		if a == "version" || a == "--version" {
			fmt.Println("orchard", version)
			return nil
		}
	}

	configPath, args, err := splitGlobalFlags(args)
	if err != nil {
		return err
	}
	cfg, loadedConfig, err := config.Load(configPath)
	if err != nil {
		return err
	}

	if len(args) == 0 {
		return tui.Run(cfg.Root, cfg.Concurrency, version)
	}

	// A bare launch with flags (e.g. `orchard --root PATH`) has no subcommand;
	// parse the TUI flags here rather than treating "--root" as a command.
	if strings.HasPrefix(args[0], "-") {
		if args[0] == "-h" || args[0] == "--help" {
			printUsage()
			return nil
		}
		root, concurrency, err := tuiFlags(args, cfg)
		if err != nil {
			return err
		}
		return tui.Run(root, concurrency, version)
	}

	switch args[0] {
	case "scan":
		return runScan(args[1:], cfg)
	case "pull":
		return runPull(args[1:], cfg)
	case "clone":
		return runClone(args[1:], cfg)
	case "preview":
		return runPreview(args[1:], cfg)
	case "config":
		if loadedConfig == "" {
			fmt.Println("config file: (none; using defaults + environment)")
		} else {
			fmt.Println("config file:", loadedConfig)
		}
		fmt.Println("root:       ", cfg.Root)
		fmt.Println("concurrency:", cfg.Concurrency)
		if cfg.Org != "" {
			fmt.Println("org:        ", cfg.Org)
		}
		return nil
	case "help", "-h", "--help":
		printUsage()
		return nil
	case "plant":
		return runPlant(args[1:], cfg)
	case "zen":
		printZen()
		return nil
	case "harvest":
		return runPull(args[1:], cfg)
	case "stats":
		return runStats(args[1:], cfg)
	case "update":
		return runUpdate()
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runScan(args []string, cfg config.Config) error {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	root := fs.String("root", cfg.Root, "folder containing git repositories")
	jsonOut := fs.Bool("json", false, "emit JSON")
	concurrency := fs.Int("concurrency", cfg.Concurrency, "maximum parallel git operations")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	repos, err := orchardgit.Scan(ctx, *root, *concurrency)
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(repos)
	}
	printRepoTable(repos)
	return nil
}

func runPull(args []string, cfg config.Config) error {
	fs := flag.NewFlagSet("pull", flag.ContinueOnError)
	root := fs.String("root", cfg.Root, "folder containing git repositories")
	all := fs.Bool("all", false, "pull all discovered repositories that pass safety checks")
	match := fs.String("match", "", "regular expression matching repo names to pull")
	jsonOut := fs.Bool("json", false, "emit JSON")
	concurrency := fs.Int("concurrency", cfg.Concurrency, "maximum parallel git operations")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !*all && *match == "" {
		return errors.New("pull requires --all or --match RE")
	}
	if _, err := regexp.Compile(*match); *match != "" && err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	repos, err := orchardgit.Scan(ctx, *root, *concurrency)
	if err != nil {
		return err
	}
	if !*all {
		repos, err = orchardgit.FilterByName(repos, *match)
		if err != nil {
			return err
		}
	}

	results := orchardgit.PullRepos(ctx, repos, *concurrency)
	if *jsonOut {
		return printJSON(results)
	}
	printPullResults(results)
	return nil
}

func runClone(args []string, cfg config.Config) error {
	fs := flag.NewFlagSet("clone", flag.ContinueOnError)
	root := fs.String("root", cfg.Root, "folder to clone repositories into")
	org := fs.String("org", cfg.Org, "GitHub organization")
	match := fs.String("match", cfg.Scope.Match, "required regular expression matching repo names")
	includeArchived := fs.Bool("include-archived", false, "include archived repositories")
	jsonOut := fs.Bool("json", false, "emit JSON")
	concurrency := fs.Int("concurrency", 4, "maximum parallel clone operations")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *org == "" {
		return errors.New("clone requires --org")
	}
	if *match == "" {
		return errors.New("clone requires --match RE to avoid cloning an entire org by accident")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	results, err := orchardgithub.CloneOrg(ctx, *org, repo.ExpandPath(*root), *match, *includeArchived, *concurrency)
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(results)
	}
	printCloneResults(results)
	return nil
}

func runPreview(args []string, cfg config.Config) error {
	fs := flag.NewFlagSet("preview", flag.ContinueOnError)
	root := fs.String("root", cfg.Root, "folder containing git repositories")
	width := fs.Int("width", 132, "preview width")
	height := fs.Int("height", 34, "preview height")
	concurrency := fs.Int("concurrency", cfg.Concurrency, "maximum parallel git operations")
	group := fs.Bool("group", false, "group rows by state")
	detail := fs.String("detail", "", "render the detail view for this repo name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var view string
	var err error
	if *detail != "" {
		view, err = tui.PreviewDetail(*root, *concurrency, *width, *height, *detail)
	} else {
		view, err = tui.Preview(*root, *concurrency, *width, *height, *group)
	}
	if err != nil {
		return err
	}
	fmt.Println(view)
	return nil
}

func printUsage() {
	fmt.Println(`orchard manages many local git repositories at once.

Usage:
  orchard [--config PATH]                  start the TUI
  orchard [--config PATH] scan [flags]     scan local repos
  orchard [--config PATH] pull [flags]     safely pull eligible repos
  orchard [--config PATH] clone [flags]    clone scoped GitHub org repos
  orchard [--config PATH] preview [flags]  render the dashboard once
  orchard [--config PATH] config           show resolved configuration
  orchard [--config PATH] stats            summarize the orchard
  orchard update                           update orchard to the latest release
  orchard version                          print the version

Common flags:
  --config PATH            config file, default ./config.yaml or user config dir
  --root PATH              folder containing repos
  --json                   emit JSON

Examples:
  orchard
  orchard preview
  orchard scan --root ~/Documents/GitHub
  orchard pull --root ~/Documents/GitHub --all
  orchard clone --root ~/Documents/GitHub --org my-org --match '^service-'`)
}

func runUpdate() error {
	current := update.Current(version)
	fmt.Printf("orchard %s, checking for updates…\n", current)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	tag, err := update.Apply(ctx, current)
	switch {
	case errors.Is(err, update.ErrAlreadyLatest):
		fmt.Println("already on the latest version")
		return nil
	case err != nil:
		return err
	default:
		fmt.Printf("updated to %s\n", tag)
		return nil
	}
}

// tuiFlags parses the flags accepted by a bare `orchard` launch (no subcommand):
// --root and --concurrency, falling back to the resolved config.
func tuiFlags(args []string, cfg config.Config) (string, int, error) {
	fs := flag.NewFlagSet("orchard", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	root := fs.String("root", cfg.Root, "folder containing git repositories")
	concurrency := fs.Int("concurrency", cfg.Concurrency, "maximum parallel git operations")
	if err := fs.Parse(args); err != nil {
		return "", 0, err
	}
	return *root, *concurrency, nil
}

func splitGlobalFlags(args []string) (string, []string, error) {
	var configPath string
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--config":
			if i+1 >= len(args) {
				return "", nil, errors.New("--config requires a path")
			}
			configPath = args[i+1]
			i++
		case strings.HasPrefix(arg, "--config="):
			configPath = strings.TrimPrefix(arg, "--config=")
		default:
			out = append(out, arg)
		}
	}
	return configPath, out, nil
}

func printRepoTable(repos []repo.Repo) {
	fmt.Printf("%-2s %-32s %-24s %-13s %9s  %s\n", "", "repo", "branch", "state", "ahead/behind", "last commit")
	for _, r := range repos {
		fmt.Printf("%-2s %-32s %-24s %-13s %4d/%-4d  %s\n",
			r.Display.Glyph(),
			truncate(r.Name, 32),
			truncate(r.Branch, 24),
			r.Display.String(),
			r.Ahead,
			r.Behind,
			truncate(r.LastCommit, 80),
		)
		if r.Err != "" {
			fmt.Printf("   %-32s %s\n", "", r.Err)
		}
	}
}

func printPullResults(results []orchardgit.PullResult) {
	for _, res := range results {
		name := res.Repo.Name
		switch res.Status {
		case orchardgit.StatusPulled:
			fmt.Printf("✓ %-32s pulled\n", name)
		case orchardgit.StatusSkipped:
			fmt.Printf("⊘ %-32s skipped: %s\n", name, res.Reason)
		case orchardgit.StatusFailed:
			fmt.Printf("× %-32s failed: %s\n", name, res.Error)
		default:
			fmt.Printf("? %-32s %s\n", name, res.Status)
		}
	}
}

func printCloneResults(results []orchardgithub.CloneResult) {
	for _, res := range results {
		switch res.Status {
		case orchardgithub.StatusCloned:
			fmt.Printf("✓ %-32s cloned to %s\n", res.Repo.Name, res.Path)
		case orchardgithub.StatusSkipped:
			fmt.Printf("⊘ %-32s skipped: %s\n", res.Repo.Name, res.Reason)
		case orchardgithub.StatusFailed:
			fmt.Printf("× %-32s failed: %s\n", res.Repo.Name, res.Error)
		default:
			fmt.Printf("? %-32s %s\n", res.Repo.Name, res.Status)
		}
	}
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	if max <= 1 {
		return s[:max]
	}
	return s[:max-1] + "…"
}

// printPlant shows a small sapling for `orchard plant` with no argument.
func printPlant() {
	fmt.Print(`
    \ | /
     \|/
      |
  ~~~~~~~~~~

  planted. go grow something worth committing.
`)
}

// runPlant clones a repo (planting a new tree) when given a URL or owner/repo,
// otherwise it just prints the sapling.
func runPlant(args []string, cfg config.Config) error {
	fs := flag.NewFlagSet("plant", flag.ContinueOnError)
	root := fs.String("root", cfg.Root, "folder to clone into")
	if err := fs.Parse(args); err != nil {
		return err
	}
	target := fs.Args()
	if len(target) == 0 {
		printPlant()
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	name, err := orchardgit.Clone(ctx, target[0], *root)
	if err != nil {
		return err
	}
	fmt.Printf("🌱 planted %s in %s\n", name, repo.ExpandPath(*root))
	return nil
}

var zenProverbs = []string{
	"Prune often. Dead branches help no one.",
	"A clean tree bears the best fruit.",
	"Commit small, commit often; a tree grows ring by ring.",
	"You cannot rush the harvest.",
	"Water your roots before you chase the fruit.",
	"Every tall tree was once a sapling that held on.",
	"Tend the orchard you have, not the one you wish for.",
	"The best time to plant a repo was twenty commits ago. The second best is now.",
	"A branch left untended grows wild.",
	"Merge gently. Force rarely.",
}

func printZen() {
	fmt.Printf("\n  \"%s\"\n\n", zenProverbs[rand.Intn(len(zenProverbs))])
}

// runStats prints a small, playful summary of the orchard.
func runStats(args []string, cfg config.Config) error {
	fs := flag.NewFlagSet("stats", flag.ContinueOnError)
	root := fs.String("root", cfg.Root, "folder containing git repositories")
	concurrency := fs.Int("concurrency", cfg.Concurrency, "maximum parallel git operations")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	repos, err := orchardgit.Scan(ctx, *root, *concurrency)
	if err != nil {
		return err
	}
	resolved := repo.ExpandPath(*root)
	if len(repos) == 0 {
		fmt.Println("nothing planted in", resolved)
		return nil
	}

	var healthy, untended, needsWater, wild int
	for _, r := range repos {
		switch r.Display {
		case repo.DisplayClean, repo.DisplayAhead, repo.DisplayFeature:
			healthy++
		case repo.DisplayDirty:
			untended++
		case repo.DisplayBehind, repo.DisplayDiverged:
			needsWater++
		default:
			wild++
		}
	}

	langCount := map[string]int{}
	for _, r := range repos {
		if s := lang.Dominant(ctx, r.Path); s.Name != "" {
			langCount[s.Name]++
		}
	}

	var freshest, thirstiest repo.Repo
	for _, r := range repos {
		if r.LastFetched.IsZero() {
			continue
		}
		if freshest.Path == "" || r.LastFetched.After(freshest.LastFetched) {
			freshest = r
		}
		if thirstiest.Path == "" || r.LastFetched.Before(thirstiest.LastFetched) {
			thirstiest = r
		}
	}

	// theme colors (auto-disabled when piped or under NO_COLOR)
	st := func(hex string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(hex)) }
	bold := func(hex string) lipgloss.Style { return st(hex).Bold(true) }
	const (
		cGreen  = "#B6F36A"
		cYellow = "#FFD56B"
		cRed    = "#FF5C7A"
		cMuted  = "#767DA8"
		cTeal   = "#5EE6D0"
	)
	count := func(n int, hex, label string) string {
		return bold(hex).Render(fmt.Sprintf("%d", n)) + st(cMuted).Render(" "+label)
	}

	fmt.Printf("\n  %s  %s\n\n", st(cGreen).Render("🌳"), st(cMuted).Render(resolved))

	tally := []string{count(len(repos), cGreen, "trees"), count(healthy, cGreen, "healthy")}
	if untended > 0 {
		tally = append(tally, count(untended, cYellow, "untended"))
	}
	if needsWater > 0 {
		tally = append(tally, count(needsWater, cRed, "need water"))
	}
	if wild > 0 {
		tally = append(tally, count(wild, cMuted, "wild"))
	}
	fmt.Println("  " + strings.Join(tally, st(cMuted).Render("   ")))

	if len(langCount) > 0 {
		type lc struct {
			name string
			n    int
		}
		var ls []lc
		for n, c := range langCount {
			ls = append(ls, lc{n, c})
		}
		sort.Slice(ls, func(i, j int) bool {
			if ls[i].n != ls[j].n {
				return ls[i].n > ls[j].n
			}
			return ls[i].name < ls[j].name
		})
		fmt.Printf("\n  %s\n", st(cMuted).Render("languages"))
		const barW = 16
		maxN := ls[0].n
		for i, l := range ls {
			if i >= 6 {
				break
			}
			f := l.n * barW / maxN
			if l.n > 0 && f < 1 {
				f = 1
			}
			bar := st(cTeal).Render(strings.Repeat("█", f)) + strings.Repeat(" ", barW-f)
			fmt.Printf("    %-12s %s %s\n", l.name, bar, bold(cTeal).Render(fmt.Sprintf("%d", l.n)))
		}
	}

	fmt.Println()
	if freshest.Path != "" {
		fmt.Printf("  %s %-26s %s\n", st(cMuted).Render(fmt.Sprintf("%-11s", "freshest")), freshest.Name, st(cMuted).Render(statsAgo(freshest.LastFetched)))
	}
	if thirstiest.Path != "" && thirstiest.Path != freshest.Path {
		fmt.Printf("  %s %-26s %s\n", st(cMuted).Render(fmt.Sprintf("%-11s", "thirstiest")), thirstiest.Name, st(cMuted).Render(statsAgo(thirstiest.LastFetched)))
	}
	targets := make([]claude.Target, 0, len(repos))
	for _, r := range repos {
		targets = append(targets, claude.Target{Name: r.Name, Path: r.Path})
	}
	if u := claude.Aggregate(targets); u.TotalSessions > 0 {
		fmt.Printf("\n  %s   %s\n", st(cMuted).Render("claude"),
			st(cMuted).Render(fmt.Sprintf("%d sessions · %d turns · %s tokens", u.TotalSessions, u.TotalTurns, humanInt(u.TotalTokens))))
	}
	if hm := harvestHeatmap(ctx, repos); hm != "" {
		fmt.Print(hm)
	}
	if hm := claudeHeatmap(repos); hm != "" {
		fmt.Print(hm)
	}
	fmt.Println()
	return nil
}

// heatWeeks is how many weeks the contribution heatmaps span.
const heatWeeks = 20

// renderHeatmap draws a GitHub-style contribution grid (weeks as columns,
// weekdays as rows) from per-day counts. Returns "" when total is 0. ramp holds
// four colors (dark -> bright); thresholds are the upper bounds for the first
// three shades, anything above uses the brightest.
func renderHeatmap(label, sub string, counts map[string]int, total int, thresholds [3]int, ramp [4]string) string {
	if total == 0 {
		return ""
	}
	st := func(hex string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(hex)) }
	const cMuted = "#767DA8"
	empty := st("#3A3F58").Render("·")
	cell := func(n int) string {
		switch {
		case n <= 0:
			return empty
		case n <= thresholds[0]:
			return st(ramp[0]).Render("■")
		case n <= thresholds[1]:
			return st(ramp[1]).Render("■")
		case n <= thresholds[2]:
			return st(ramp[2]).Render("■")
		default:
			return st(ramp[3]).Render("■")
		}
	}
	now := time.Now()
	first := now.AddDate(0, 0, -int(now.Weekday())).AddDate(0, 0, -7*(heatWeeks-1))
	labels := []string{"", "Mon", "", "Wed", "", "Fri", ""}

	var b strings.Builder
	fmt.Fprintf(&b, "\n  %s   %s\n", st(cMuted).Render(label), st(cMuted).Render(sub))
	for row := 0; row < 7; row++ {
		fmt.Fprintf(&b, "  %-4s", labels[row])
		for col := 0; col < heatWeeks; col++ {
			day := first.AddDate(0, 0, col*7+row)
			if day.After(now) {
				b.WriteString(" ")
				continue
			}
			b.WriteString(cell(counts[day.Format("2006-01-02")]))
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "  %-4s%s %s%s%s%s%s %s\n", "", st(cMuted).Render("less"),
		empty, st(ramp[0]).Render("■"), st(ramp[1]).Render("■"), st(ramp[2]).Render("■"), st(ramp[3]).Render("■"),
		st(cMuted).Render("more"))
	return b.String()
}

// harvestHeatmap is your own commits per day over the last heatWeeks weeks.
func harvestHeatmap(ctx context.Context, repos []repo.Repo) string {
	since := fmt.Sprintf("%d weeks ago", heatWeeks+1)
	counts := map[string]int{}
	total := 0
	for _, r := range repos {
		for _, d := range orchardgit.AuthoredDays(ctx, r.Path, since) {
			counts[d]++
			total++
		}
	}
	return renderHeatmap("harvest", fmt.Sprintf("%d commits in the last %d weeks", total, heatWeeks),
		counts, total, [3]int{2, 5, 9}, [4]string{"#356E3F", "#5FA052", "#8FD15A", "#B6F36A"})
}

// claudeHeatmap is your Claude Code turns per day over the last heatWeeks weeks.
func claudeHeatmap(repos []repo.Repo) string {
	cutoff := time.Now().AddDate(0, 0, -7*heatWeeks)
	counts := map[string]int{}
	total := 0
	for _, r := range repos {
		for _, s := range claude.Sessions(r.Path, 0) {
			if s.Modified.Before(cutoff) {
				continue
			}
			counts[s.Modified.Format("2006-01-02")] += s.Assistant
			total += s.Assistant
		}
	}
	return renderHeatmap("claude", fmt.Sprintf("%d turns in the last %d weeks", total, heatWeeks),
		counts, total, [3]int{20, 50, 100}, [4]string{"#7A4A1E", "#B5742E", "#E0973F", "#FF9E64"})
}

// humanInt formats a large count compactly: 1234 -> "1.2k", 1234567 -> "1.2M".
func humanInt(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func statsAgo(t time.Time) string {
	switch d := time.Since(t); {
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
