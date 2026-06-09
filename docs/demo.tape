# orchard demo recording (VHS) - run from the repo root:
#   go build -ldflags "-X main.version=$(git describe --tags --abbrev=0)" -o /tmp/orchard-demo-bin .
#   vhs docs/demo.tape
#   ffmpeg -y -i demo.mp4 -filter_complex \
#     "[0:v]fps=12,split[a][b];[a]palettegen=stats_mode=diff[p];[b][p]paletteuse=dither=bayer:bayer_scale=4" \
#     docs/screenshots/demo.gif && rm demo.mp4
# The GIF is derived from the mp4 because VHS's own GIF encoder leaves frame-diff
# artifacts at this size. ORCHARD_DEMO=1 keeps the recording to fictional repos;
# ORCHARD_AI_CMD=claude so the agent features render without a real claude on PATH.

Output demo.mp4

Set Shell "zsh"
Set FontFamily "JetBrainsMono Nerd Font"
Set FontSize 15
Set Width 1500
Set Height 820
Set Padding 18
Set Theme "Catppuccin Mocha"
Set WindowBar Colorful
Set TypingSpeed 55ms

# Warm-up run (hidden): the terminal settles its font/size during this launch so
# the visible run below renders the intro with no startup reflow artifacts.
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
Show
Enter

# 1. Launch: the grove grows in, then the dashboard cascades into place
#    (AGENT column with Claude/Codex marks, GR graph badges, usage panel).
Sleep 5s

# 2. Repo detail: the regrouped Claude Code section, code graph, files touched.
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
