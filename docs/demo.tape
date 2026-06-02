# orchard demo recording (VHS) - run from the repo root:  vhs docs/demo.tape
# ORCHARD_DEMO=1 keeps the recording to fictional repos; ORCHARD_AI_CMD=claude so
# the Claude features render without needing a real claude on PATH.

Output docs/screenshots/demo.gif

Set Shell "zsh"
Set FontFamily "JetBrainsMono Nerd Font"
Set FontSize 15
Set Width 1500
Set Height 820
Set Padding 18
Set Theme "Catppuccin Mocha"
Set WindowBar Colorful
Set TypingSpeed 55ms

Hide
Type "ORCHARD_DEMO=1 ORCHARD_AI_CMD=claude ORCHARD_NO_UPDATE_CHECK=1 /Users/prakash.kurup/.local/bin/orchard"
Enter
Sleep 2.5s
Show

# 1. The dashboard: every repo on one screen, the Claude column, the usage panel.
Sleep 3s

# 2. Repo detail: the regrouped Claude Code section and the files Claude touched.
Enter
Sleep 3.5s

# 3. Files view (f): everything the agent read or edited, uncommitted flagged.
Type "f"
Sleep 2.5s
Escape
Sleep 400ms
Escape
Sleep 800ms

# 4. Working-tree diff (d): colorized, vs HEAD.
Type "d"
Sleep 2.5s
Escape
Sleep 800ms

# 5. Draft a commit message with Claude (M): the draft animates, then the message.
Type "M"
Sleep 4s
Escape
Sleep 800ms

# 6. Search code across every repo (S).
Type "S"
Sleep 600ms
Type "checkout"
Sleep 500ms
Enter
Sleep 3s
Escape
Sleep 800ms

# 7. Workspace presets (W): save and launch a set of repos in one keystroke.
Space
Down
Space
Sleep 500ms
Type "W"
Sleep 2.5s
Escape
Sleep 800ms

Type "q"
Sleep 500ms
