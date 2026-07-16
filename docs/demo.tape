# docs/demo.tape — source for assets/demo.gif.
#
# Render locally:  make build && vhs docs/demo.tape
# CI regenerates it via .github/workflows/demo.yml.
#
# Requires vhs (https://github.com/charmbracelet/vhs). It drives Marga's built-in
# offline demo mode (`marga --demo`), which needs no config, credentials, or
# network — so the demo is fully reproducible, in CI or locally.

Output assets/demo.gif

Set Shell "bash"
Set FontSize 16
Set Width 1200
Set Height 700
Set Padding 20
Set Theme "Dracula"
Set TypingSpeed 60ms

# Match Marga's theme to the terminal recording theme.
Hide
Type "export MARGA_THEME=dracula" Enter
Type "clear" Enter
Show

Type "./bin/marga --demo" Enter
Sleep 3s

Type "Marga unifies Discord and Matrix in one terminal UI" Enter
Sleep 1500ms

# Jump to the end-to-end encrypted room.
Type "/join #e2ee-room" Enter
Sleep 2s
Type "end-to-end encrypted, right in the terminal" Enter
Sleep 1500ms

# Switch to the other network (Ctrl+T) — the multi-network superpower.
Ctrl+T
Sleep 2500ms
Type "/join #dev" Enter
Sleep 1500ms
Type "one keystroke to switch networks" Enter
Sleep 2s

# Show the help overlay, then dismiss.
Ctrl+H
Sleep 2500ms
Escape
Sleep 1s
