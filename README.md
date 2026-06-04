# orchard

An agent-aware, multi-repo terminal dashboard. See every local git repo on one screen (branch, dirty / ahead / behind, language, last commit) plus what your AI coding agent did across them: where Claude Code ran, the files it touched, and a flag when there is uncommitted AI work. Then pull, fetch, switch branches, search, diff, or open any repo in your editor, browser, or Claude Code, all from the keyboard.

![orchard demo](docs/screenshots/demo.gif)

## Install

### Requirements

- **Platforms:** macOS and Linux (amd64/arm64). Windows is not supported yet.
- **Runtime:** `git` on your `PATH`.
- **Build:** Go **1.24+** only if you install from source or with `go install`.
- **Optional:**
  - A [Nerd Font](https://www.nerdfonts.com/) for the icons (any other monospace font works; icons just render as boxes).
  - [`gh`](https://cli.github.com/) (GitHub CLI) - used by `orchard clone --org` for auth if `GITHUB_TOKEN` isn't set.
  - [`claude`](https://claude.com/claude-code) - for the `c` key (launch Claude Code) and the usage panel.
  - A supported terminal for new-tab launches: Ghostty, iTerm2, WezTerm, tmux, or macOS Terminal (otherwise it falls back to running in place).

### Download a release (no Go required)

Grab the archive for your OS/arch from the [**Releases**](https://github.com/prakashkurup/orchard/releases) page, extract it, and move the `orchard` binary onto your `PATH`:

```sh
# e.g. macOS Apple Silicon - adjust the filename for your OS/arch
tar -xzf orchard_*_darwin_arm64.tar.gz
sudo mv orchard /usr/local/bin/
orchard version
```

On macOS the binary is unsigned, so the first launch may be blocked by Gatekeeper. Allow it once with:

```sh
xattr -d com.apple.quarantine /usr/local/bin/orchard    # or right-click → Open
```

### go install (needs the Go toolchain)

```sh
go install github.com/prakashkurup/orchard@latest
```

### From source

```sh
git clone https://github.com/prakashkurup/orchard.git
cd orchard
make build       # produces ./orchard (version stamped from git tags)
make install     # installs into $(go env GOPATH)/bin
```

### Updating

orchard checks GitHub for a newer release at most once a day and shows a small notice when one is available (one anonymous request; opt out with `ORCHARD_NO_UPDATE_CHECK=1`).

```sh
orchard update     # download + checksum-verify + replace the binary in place
```

`orchard update` is for release-binary installs. If you installed with `go install`, run `go install github.com/prakashkurup/orchard@latest` instead (the command will tell you), and `orchard update` will say so for Homebrew/go-install builds rather than touching a managed binary.

## Quick start

```sh
orchard                               # launch the TUI for the configured root
orchard --root ~/Documents/GitHub     # or point it at any folder
orchard preview                       # render one dashboard frame to stdout
orchard scan --root ~/code --json     # machine-readable status of every repo
orchard pull --root ~/code --all      # fast-forward every eligible repo
```

`orchard` scans **one root folder** and treats each immediate subdirectory that contains a `.git` as a repository. (If the root itself is a repo, it shows just that one.)

The root defaults to your **current directory**, so `cd ~/code && orchard` just works. To make a root persistent, see [Configuration](#configuration).

## Why orchard

Most tools either manage many repos or inspect AI assistant history. orchard combines both: an interactive multi-repo git dashboard that knows where you have been running Claude Code, launches it in any repo, and searches code across every repo at once.

If you know the space, `gita`, `mani`, `gitpane`, `git-dash`, and `git-scope` cover multi-repo git workflows; `ccusage` and `claude-history` cover Claude usage and history. orchard puts the local repo dashboard and Claude-aware workflow in the same window.

If you juggle many repositories and lean on Claude Code, orchard is the cockpit for that workflow. If you only ever work in one repo at a time, a single-repo TUI like `lazygit` will serve you better.

## Features

- **One-screen overview** of every repo: branch, clean/dirty/ahead/behind/diverged/detached, uncommitted + stash counts, dominant language, last-synced and last-commit times (color-coded by freshness).
- **Safe bulk pull** - fast-forward only; skips dirty repos, detached HEADs, and repos with no upstream (a non-fast-forward is reported, never forced).
- **Preview docs** (`v`) - render a repo's `CLAUDE.md` / `AGENTS.md` / `README.md` in the terminal, with its size and an estimated per-session token cost.
- **Fetch, branch switch, multi-select** - act on one repo or many at once.
- **Open anywhere** - launch your editor (`e`), the repo in your browser (`O`), or **Claude Code** (`c`) in a new terminal tab; multi-repo actions ask for confirmation first.
- **Cross-repo code search** (`S`) that respects `.gitignore`.
- **GitHub aware** - open PR count and CI status per repo (when a token is set), shown in the detail view, with a failing-CI flag on the dashboard.
- **Worklog** (`L`) - your own commits across all repos in a time window.
- **Clone** (`+`) a new repo into the dashboard, or `orchard clone` a scoped GitHub org.
- **Claude Code aware** - usage panel, a per-repo activity column, launch/resume/cross-repo sessions, and a flag for uncommitted AI work. See [Claude Code](#claude-code) below.
- **Scriptable CLI** - `scan`, `pull`, `clone` with `--json` for cron and pipelines.

## Claude Code

orchard treats Claude Code as a first-class part of a multi-repo workflow. Everything is read locally from your `~/.claude` transcripts (or `$CLAUDE_CONFIG_DIR` if you have relocated them); nothing is sent anywhere.

### See activity

- **Usage panel** pinned under the list: total sessions, turns, repos used, last active, a model split, and your busiest repos.
- **Per-repo CLAUDE column**: how long ago Claude last ran in each repo, colored by recency.
- **Active now**: a session writing in the last ~60s shows `● live` (green); if that repo is also dirty it shows `!live` (red), so live AI work on an uncommitted tree stands out.
- **Per-repo footprint in the detail view** (`enter`): that repo's recent sessions, total turns and tokens, and when Claude last ran there.
- **Files Claude touched** (detail view): the files Claude read or edited here, edited-first, with files it changed but hasn't committed flagged. Press `f` for the full list, then `enter` to open one in your editor or `d` to diff just that file.
- **Uncommitted-work flag**: when a repo is dirty *and* Claude ran there recently, the CLAUDE cell turns red with a `!`, so AI edits never get lost in an unstaged tree.
- **Sort by Claude** (`s`): float the most recently Claude-worked repos to the top.
- **Usage**: per-repo token totals in the panel and session picker; `orchard stats` shows total sessions, turns, tokens, and a Claude activity heatmap alongside the commit harvest.

![the repo detail view: languages, the Claude Code section (activity, context, recent sessions, and the files Claude touched), GitHub PRs, the working tree, and the commit graph](docs/screenshots/details.png)

![the Files view (`f`): every file Claude read or edited in a repo, edited first, with uncommitted ones flagged; open one in your editor or diff just that file](docs/screenshots/files.png)

### Resume and search sessions

- **Resume** (`C`): continue the most recent session in the current repo (`claude --continue`).
- **Session history** (`H`): browse a repo's past sessions by their titles (turn count, model, age) and resume any one (`claude --resume <id>`).
- **Search past sessions** (`R`): search the *content* of every repo's Claude sessions at once and resume the match in its own repo. For deep semantic search and a full conversation viewer, the dedicated [`claude-history`](https://github.com/raine/claude-history) is excellent; orchard's `R` is a quick cross-repo find-and-resume without leaving the dashboard.

### Launch Claude across repos

- **Launch** (`c`): open Claude Code in a new terminal tab for the selected repo(s); multi-repo asks for confirmation.
- **Across repos** (`A`): one session spanning the selected repos, opened in the first with the rest attached via `--add-dir`, for cross-service work. `space`-select 2+ first. Each added repo's `CLAUDE.md` is loaded too (orchard sets `CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD=1`; opt out with `ORCHARD_ADDDIR_MEMORY=0`), so the session has the *instructions* for every repo, not just file access.
- **Workspace presets** (`W`): save a `space`-selected repo set under a name, then launch the cross-repo `A` session on it in one keystroke, for the groups of services you keep working on together. Stored in `presets.json` (see [Files orchard writes](#files-orchard-writes)).

![workspace presets (`W`): save a selected repo set and launch a cross-repo session on it](docs/screenshots/presets.png)

### Draft commit messages

- **Draft a commit message** (`M`): Claude Code drafts a message from the working-tree diff *headlessly* (`claude -p`) and shows it in a window you can copy (`y`) or regenerate (`r`), without leaving the dashboard. Other assistants fall back to a terminal session.

![drafting a commit message with Claude (`M`): the message appears in a window to copy or regenerate](docs/screenshots/commit.png)

### Keep instructions healthy

- **Instructions health** (detail view + the `needs-md` filter): shows which repos have a `CLAUDE.md` / `AGENTS.md`, and flags repos where Claude runs with no project context, either no `CLAUDE.md` at all, or an `AGENTS.md` Claude won't read (Claude Code doesn't load `AGENTS.md` natively). `I` wires the latter up by creating a `CLAUDE.md` that imports `@AGENTS.md` (only where there's no `CLAUDE.md` yet; it never edits an existing one).

### Adapt to your setup

- **Adaptive**: launches `$ORCHARD_AI_CMD` if set, otherwise the first of `claude` or `copilot` found on your `PATH`.

### Claude Code panel or column empty?

The usage panel and CLAUDE column reflect repos you have **actually run Claude Code in** (as the working directory), not just that Claude is installed. If they look empty:

- **Use Claude in a repo first.** Run `claude` inside one of the repos once; the panel and column fill from past sessions, not from installation.
- **Point orchard at the right root.** They only cover repos under orchard's root, so set `--root` / `ORCHARD_ROOT` to where you actually work with Claude.
- **Relocated config?** orchard reads `~/.claude`, or `$CLAUDE_CONFIG_DIR` when you have moved Claude's config there.
- **The `c` launcher is missing?** orchard needs `claude` on your `PATH` (a shell alias or function does not count) or `ORCHARD_AI_CMD=/full/path/to/claude`. GUI launches often have a thinner `PATH` than your terminal.

## TUI keys

Press `?` in-app for the same keymap and legend.

### Navigate and select

| Key | Action |
|-----|--------|
| `↑ ↓` / `j k` | move · `g` / `G` top / bottom |
| mouse | click a row to select, wheel to scroll (disable with `ORCHARD_NO_MOUSE=1`) |
| `n` | jump to next repo with new commits since last visit |
| `space` / `a` | select current / select all visible |
| `x` | clear all selections |
| `y` | copy the current repo's path to the clipboard |

### Git and repo actions

| Key | Action |
|-----|--------|
| `enter` | repo detail (languages, instructions, GitHub PR/CI, Claude footprint, working tree, commit graph, remotes) |
| `d` | view the working-tree diff (vs HEAD) |
| `p` / `P` | pull selected / all (fast-forward only) |
| `f` / `F` | fetch selected / all |
| `b` | switch branch |
| `e` / `E` | open in editor / change default editor |
| `O` | open repo(s) in browser (confirms for >1 repo) |
| `+` | clone a repo into the dashboard |

### Claude actions

| Key | Action |
|-----|--------|
| `c` | open Claude Code in a new tab (confirms for >1 repo) |
| `C` | resume the last Claude Code session in the current repo |
| `H` | browse past Claude Code sessions for the current repo and resume any one |
| `R` | search the *content* of past Claude Code sessions across all repos, then resume one |
| `A` | one Claude Code session across the selected repos: opens in the first selected, the rest attached via `--add-dir` (`space`-select 2+ first) |
| `M` | draft a commit message (Claude Code drafts it in a window to copy / regenerate) |
| `I` | wire `AGENTS.md` into a new `CLAUDE.md` for the selected repos (so Claude reads it) |
| `W` | workspace presets: save a repo set, then launch a cross-repo session (`A`) on it |
| `v` | preview a repo's `CLAUDE.md` / `AGENTS.md` / `README.md`, rendered, with size and est. token cost |

### Search, stats, and filtering

| Key | Action |
|-----|--------|
| `S` | search code across all repos |
| `L` | worklog - your commits across repos |
| `T` | stats page (languages, freshest/thirstiest, Claude usage, harvest + Claude heatmaps) |
| `/` | filter by text, or a `branch:` / `name:` prefix |
| `tab` | cycle quick filters (attention, dirty, behind, feature, at-risk, ai-touched, needs-md) |
| `s` / `o` | cycle sort (attention / name / synced / claude) / toggle grouping |

### App

| Key | Action |
|-----|--------|
| `r` / `w` | refresh now / toggle live auto-refresh (also fetches remotes periodically; tune with `ORCHARD_FETCH_SECS`) |
| `?` / `q` | help & legend / quit |

## CLI

```
orchard [--config PATH]                  start the TUI
orchard [--config PATH] scan    [flags]  scan local repos
orchard [--config PATH] pull    [flags]  safely pull eligible repos
orchard [--config PATH] clone   [flags]  clone scoped GitHub org repos
orchard [--config PATH] preview [flags]  render the dashboard once
orchard [--config PATH] config           show resolved configuration
orchard [--config PATH] stats            summarize the orchard
orchard update                           update orchard to the latest release
orchard version                          print the version
orchard help
```

Common flags: `--root PATH`, `--concurrency N`. `--json` is supported by `scan`, `pull`, and `clone` only. Run `orchard <cmd> -h` for per-command flags.

- `scan` - `--root`, `--concurrency`, `--json`
- `pull` - `--all` **or** `--match RE`, `--root`, `--concurrency`, `--json`
- `clone` - `--org` and `--match RE` (both required, so you never clone a whole org by accident), `--include-archived`, `--root`, `--concurrency`, `--json`
- `preview` - `--root`, `--concurrency`, `--width`, `--height`, `--group`, `--detail NAME`

## Configuration

All settings are optional; the default root is your current directory, so the simplest setup is no config at all.

If you only need to tell orchard where your repos live, pick one:

1. Run it from that folder: `cd ~/code && orchard`
2. Pass a root for one launch: `orchard --root ~/code`
3. Persist the root with your shell: `export ORCHARD_ROOT=~/code`
4. Use a config file when you also want saved clone defaults like `org` / `scope`

**Precedence:** CLI flags → environment variables → config file → built-in defaults.

The `orchard config` **command** doesn't change anything: it prints the resolved settings (root, concurrency, and which config file, if any) so you can confirm what orchard will use.

### Environment variables

- `ORCHARD_ROOT` - default repo folder.
- `ORCHARD_CONFIG` - explicit config file path.
- `GITHUB_TOKEN` - token for `orchard clone` (falls back to `gh auth token`).
- `ORCHARD_ADDDIR_MEMORY` - set to `0` to stop a cross-repo Claude session (`A`) from loading the added repos' `CLAUDE.md` (on by default).
- `ORCHARD_NO_UPDATE_CHECK` - set to `1` to disable the daily check for a newer release.
- `ORCHARD_NO_MOUSE` - set to `1` to disable mouse capture (so your terminal's native text selection works).
- `ORCHARD_FETCH_SECS` - while live refresh is on, how often (seconds) to fetch remotes in the background so ahead/behind stay current. Default `300` (5 min); `0` disables background fetching (fetch on demand with `f` / `F`).
- `ORCHARD_NO_ANIM` - set to `1` to turn off motion (the launch intro, the count-up, eased scrolling, and the "just tended" row pulse).

### Advanced config file

The config file is a small `key: value` format with one nested `scope:` section. Copy [`config.example.yaml`](config.example.yaml) to `config.yaml`. Search order: `--config` → `$ORCHARD_CONFIG` → `./config.yaml` → next to the binary → `$XDG_CONFIG_HOME/orchard/config.yaml`.

```yaml
root: ~/Documents/GitHub
concurrency: 8
org: your-github-org
scope:
  match: "^(service|app)-"
```

### Files orchard writes

Beyond the optional `config.yaml`, orchard keeps a little state under your config dir (`$XDG_CONFIG_HOME/orchard`, or `~/Library/Application Support/orchard` on macOS), created lazily the first time it is needed:

- `presets.json` - your workspace presets (`W`).
- `seen.json` - the last time you visited each repo, for the `n` "new commits since last visit" jump.
- `editor` - your chosen default editor (`E`).

These are plain, hand-editable files that orchard owns. Reads are **fail-soft**: if one is missing or corrupt, orchard falls back to a safe default (no presets, no last-visit, the editor picker) instead of erroring, and the next save rewrites it. The daily update check also caches its last result under your home cache dir. A malformed `config.yaml`, by contrast, is reported at startup, since that is configuration you declared on purpose.

## Security & privacy

orchard runs locally with no telemetry. Network traffic only happens when you ask for it:

- `git` talking to your remotes (fetch / pull / clone).
- the **GitHub API** for open-PR and CI status, and for `orchard clone`.
- **Claude Code**, when you launch it (`c` / `C` / `H` / `A`) or draft a commit message (`M`, which runs `claude -p` and sends the working-tree diff to Claude). The usage panel, CLAUDE column, and stats only **read** your local `~/.claude` transcripts - nothing is sent for those.
- the **update check** - one anonymous request to the GitHub releases API, at most once a day (disable with `ORCHARD_NO_UPDATE_CHECK=1`).

Privacy details:

- The GitHub token is read from `GITHUB_TOKEN` or `gh auth token` at the moment it's needed; it is **never written to disk or printed**, and tokens embedded in a remote URL are stripped before the URL is shown or opened.
- Pulls are **fast-forward only** and skip dirty repos - orchard never force-pushes, rebases, or discards work, and it never commits or pushes on your behalf.

See [SECURITY.md](SECURITY.md) for how to report a vulnerability.

## Development

Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) and [Lipgloss](https://github.com/charmbracelet/lipgloss).

```sh
make build       # build ./orchard
make test        # run the test suite
make test-race   # tests with the race detector
make lint        # gofmt check + go vet
make check       # lint + test (run before committing)
make cover       # HTML coverage report
make help        # list all targets
```

The codebase is split into small packages under `internal/` (`tui`, `git`, `github`, `repo`, `config`, `editor`, `lang`, `search`, `seen`, `termlaunch`, `claude`) with a thin `main.go` CLI. The logic lives in the unit-tested packages; the TUI is a Bubble Tea layer on top.

### Cutting a release

Releases are automated with [GoReleaser](https://goreleaser.com/). Tag a commit and push the tag - the `release` workflow builds binaries for macOS and Linux (amd64 + arm64) and publishes them:

```sh
git tag v0.1.0
git push origin v0.1.0
```

## Contributing

Issues and PRs are welcome. Please run `make check` before submitting. See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[MIT](LICENSE)
