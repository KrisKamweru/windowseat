# Icons

The icon is a plane window with a gauge inside. Codex image generation drew `source/windowseat-master.png`.

`render.py` snaps every pixel of the master to navy, paper or teal, then swaps the teal arc for each state colour: blue for ok, orange for warning, grey for stale and pink for error. It writes PNGs at 16, 32, 64 and 256px, a 16/32/48 `.ico` per state, and `favicon.ico`. The tray embeds `state-*-64.png` on Linux and macOS and `state-*.ico` on Windows.

Regenerate with `python3 assets/icon/render.py`, then run `go generate ./cmd/...` so the Windows executables pick up the new icon.
