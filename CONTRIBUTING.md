# Contributing to orchard

Thanks for your interest! orchard is a small, focused Go project and PRs are welcome.

## Getting started

```sh
git clone https://github.com/prakashkurup/orchard.git
cd orchard
make build      # build ./orchard
make check      # gofmt + go vet + race tests + build - run this before pushing
```

You need **Go 1.24+** and `git`.

## Guidelines

- **Run `make check` before opening a PR.** CI runs the same checks (`make lint test-race build`).
- **Keep it formatted:** `gofmt` is the law (`make fmt`). `go vet` must be clean.
- **Add tests** for new behavior. The logic lives in unit-tested packages under `internal/`; please keep it that way and keep the Bubble Tea `tui` layer thin.
- **Small packages, small functions.** Match the style of the surrounding code.
- **No new runtime dependencies** without discussion.

## Project layout

```
main.go                CLI entry point (scan / pull / clone / preview / TUI)
internal/tui/          Bubble Tea dashboard, modals, rendering
internal/git/          git CLI wrapper (scan, status, pull, fetch, branches)
internal/github/       GitHub org listing + cloning
internal/repo/         Repo model + derived display state + discovery
internal/config/       config file + env resolution
internal/editor/       editor detection & launch
internal/lang/         language detection from tracked files
internal/search/       cross-repo code search
internal/seen/         "since last visit" tracking
internal/termlaunch/   open a new terminal tab/window per emulator
internal/claude/       read local Claude Code usage transcripts
```

## Reporting bugs / ideas

Open an issue with steps to reproduce (and your OS + terminal for rendering bugs). For security reports, see [SECURITY.md](SECURITY.md).
