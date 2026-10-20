# Icons

`windowseat.svg` is the source: a plane window (rounded rect) with a gauge inside (arc, needle, hub). Codex image generation drew the design. It was traced to SVG, then redrawn as four shapes fitted to the trace.

The teal arc, `#18B9AC`, is the only colour that changes. `render` swaps it for each state: blue for ok, orange for warning, grey for stale and pink for error. It writes PNGs at 16, 32, 64 and 256px, a 16/32/48 `.ico` per state, and `favicon.ico`. The tray embeds `state-*-64.png` on Linux and macOS and `state-*.ico` on Windows.

After editing the SVG, run these from the repository root:

```sh
go run ./assets/icon/render
go generate ./cmd/...
```

The second command rebuilds the icon and version resources inside the Windows executables.
