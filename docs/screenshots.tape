# Static README screenshots (VHS) - run from the repo root:
#   go build -ldflags "-X main.version=$(git describe --tags --abbrev=0)" -o /tmp/orchard-demo-bin .
#   vhs docs/screenshots.tape
# Captures the detail view, files view, commit-message window, and presets in
# ORCHARD_DEMO=1 fictional-data mode. The throwaway gif output is required by
# VHS; the screenshots are the real artifacts. Delete screenshots-scratch.gif after.

Output screenshots-scratch.gif

Set Shell "zsh"
Set FontFamily "JetBrainsMono Nerd Font"
Set FontSize 15
Set Width 1500
Set Height 820
Set Padding 18
Set Theme "Catppuccin Mocha"
Set WindowBar Colorful

Hide
Type "ORCHARD_DEMO=1 ORCHARD_AI_CMD=claude ORCHARD_NO_UPDATE_CHECK=1 /tmp/orchard-demo-bin"
Enter
Sleep 4s
Type "q"
Sleep 1s
Type "clear"
Enter
Sleep 500ms
Type "ORCHARD_DEMO=1 ORCHARD_AI_CMD=claude ORCHARD_NO_UPDATE_CHECK=1 /tmp/orchard-demo-bin"
Enter
Sleep 4s
Show

# detail view: languages, AI readiness, code graph, Claude + Codex footprints
Enter
Sleep 3s
Screenshot docs/screenshots/details.png

# files view: everything the agent read or edited
Type "f"
Sleep 2s
Screenshot docs/screenshots/files.png
Escape
Sleep 400ms
Escape
Sleep 800ms

# commit-message window (drafted headlessly in demo mode)
Type "M"
Sleep 3s
Screenshot docs/screenshots/commit.png
Escape
Sleep 800ms

# workspace presets over a two-repo selection
Space
Down
Space
Sleep 400ms
Type "W"
Sleep 2s
Screenshot docs/screenshots/presets.png
Escape
Sleep 500ms

Type "q"
Sleep 400ms
