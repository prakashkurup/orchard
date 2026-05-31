# Security Policy

## Reporting a vulnerability

Please report security issues **privately** rather than opening a public issue:

- Use GitHub's [private vulnerability reporting](https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability) ("Report a vulnerability" under the repo's **Security** tab), or
- email the maintainer.

I'll acknowledge within a few days and aim to ship a fix promptly.

## Scope & threat model

orchard is a **local, single-user developer tool**. It runs on your machine and shells out to `git`, `gh`, your browser, your editor, and your terminal emulator. It has no telemetry and no server component.

Relevant areas to consider when reporting:

- **Argument / command injection** via repository names, branch names, remote URLs, or pasted clone URLs reaching `git`/`open`/editor/`osascript` invocations.
- **Token handling** - `GITHUB_TOKEN` / `gh auth token` must never be logged, printed, or written to disk.
- **Path handling** - clone destinations derived from remote repository names.

## Good to know

- The GitHub token is fetched only when needed and is never persisted by orchard.
- The Claude usage panel only **reads** local `~/.claude` files.
- `pull` is fast-forward only and skips dirty repositories.
