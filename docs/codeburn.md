# CodeBurn usage in orchard

orchard can use [CodeBurn](https://github.com/getagentseal/codeburn) as its cost
and usage engine while keeping the display native to orchard's Bubble Tea TUI.
CodeBurn discovers local agent sessions, normalizes token classes, maintains
model pricing, and classifies activity; orchard reads its client-facing JSON and
renders the result alongside repository and agent state.

## Setup

Install a persistent CodeBurn command (0.9.20 or newer is recommended):

```sh
npm install -g codeburn
# or
brew install codeburn
```

Run orchard normally. When `codeburn` is on `PATH`, the dashboard adds a compact
cost strip. Press `U` for the full usage page, then use left/right or `1`-`6` to
switch between Today, 7 Days, 30 Days, This Month, 6 Months, and Lifetime.

The report is scoped to orchard's configured root, so a dashboard rooted at
`~/Documents/GitHub` does not include projects outside that directory.

## Integration boundary

orchard starts one resident child process without a shell:

```text
codeburn serve --stdio
```

It sends serialized read-only requests for CodeBurn's `menubar-json` contract.
This is the same boundary used by CodeBurn's native clients and avoids paying
the process and session-cache startup cost on every refresh. If the resident
protocol is unavailable, orchard falls back to a one-shot read:

```text
codeburn status --format menubar-json --period today --provider all \
  --project <orchard-root> --no-optimize --no-timeline
```

Unknown JSON fields are ignored. If critical fields are missing or invalid,
orchard hides the cost strip and preserves its native Claude/Codex token view.
Provider, parser, classification, cache, and pricing updates remain CodeBurn's
responsibility rather than being copied into orchard.

orchard's existing local readers remain authoritative for features CodeBurn
does not expose through this contract: resuming and searching sessions, touched
files, live-agent state, and per-repository agent footprints.

## Configuration

`ORCHARD_CODEBURN` controls executable discovery:

- unset or `auto`: use `codeburn` from `PATH` when available;
- `/absolute/path/to/codeburn`: use an explicit executable (helpful for GUI
  launches whose `PATH` does not include asdf, Homebrew, or npm shims);
- `off`, `0`, or `false`: disable the integration.

orchard never invokes `npx`, downloads CodeBurn, mutates CodeBurn configuration,
or sends usage data over the network. CodeBurn itself reads local session files
and owns its pricing cache. Closing orchard closes the sidecar's stdin, which is
CodeBurn's clean shutdown signal.
