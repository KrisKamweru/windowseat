# Windowseat

Windowseat shows how much quota you have left on Claude Code, Codex and GitHub Copilot. It asks each provider's own CLI for the numbers and reports them to you, to scripts and to coding agents.

It only reads. It never decides whether an agent should keep working, never switches accounts and never spends a reset credit.

## What it runs

- `windowseat serve` keeps a cached reading of every provider and refreshes it on a schedule.
- `windowseat usage` prints the current reading. Add `--json` for scripts, `--provider claude|codex|copilot` for one provider, or `--refresh` for a fresh read. Without a running service it reads the providers directly.
- `windowseat health` prints service and provider health.
- `windowseat mcp` runs a read-only MCP server on stdio, so agents can call `get_usage`.
- `windowseat-tray` puts a gauge icon in the system tray. It turns amber when a meter passes `--warn-at` (default 80% used).

The agent skill in [`skills/windowseat`](skills/windowseat/SKILL.md) tells an agent how to read and interpret the output.

## How it reads

| Provider | Source |
| --- | --- |
| Claude Code | `claude -p /usage --output-format stream-json`, which sends no model prompt |
| Codex | `account/rateLimits/read` on a read-only `codex app-server` |
| Copilot | `account.getQuota` over JSON-RPC to `copilot --headless --stdio` |

Windowseat never opens credential files, keychains or tokens. Each provider CLI gets the environment unchanged and handles its own sign-in. The Codex and Copilot adapters only send methods on a fixed allowlist, so they can't reach the methods that spend reset credits or return credentials.

If a read fails, Windowseat reports the failure and keeps the last good reading marked as stale. It never shows a failed read as 0%.

## Platforms

| Platform | Status |
| --- | --- |
| Linux, including WSL | Tested |
| Windows 10 and 11 | Provider reads tested. The tray icon hasn't been checked yet |
| macOS | Untested |

macOS builds are untested because I don't have a Mac to test on. The code is written to work there, but expect rough edges. Bug reports and fixes from Mac users are welcome.

## Build

You need Go 1.26 or newer.

```sh
scripts/build.sh                    # linux/amd64, windows/amd64, windows/arm64
scripts/build.sh darwin/arm64       # any GOOS/GOARCH pair
```

Binaries land in `dist/<os>-<arch>/`. The tray on macOS needs CGO, so build it on a Mac.

## Install

Run `install` from the build output. It copies both binaries into your user's program folder, starts the service and the tray, and sets both to start when you log in. Nothing needs admin rights.

```sh
dist/linux-amd64/windowseat install             # service and tray
dist/linux-amd64/windowseat install --no-tray   # service only
dist/linux-amd64/windowseat install --dry-run   # print the steps, change nothing
windowseat uninstall                            # undo all of it; also takes --dry-run
```

On Windows, run it from PowerShell in the folder that holds both `.exe` files. PowerShell needs the `.\` prefix to run a program from the current folder:

```powershell
.\windowseat.exe install
.\windowseat.exe install --no-tray
& "$env:LOCALAPPDATA\Programs\Windowseat\windowseat.exe" uninstall
```

Running `install` again upgrades in place. It stops the old copies before it replaces them.

| Platform | Binaries | Starts at login through |
| --- | --- | --- |
| Linux | `~/.local/bin` | `systemd --user` unit `windowseat.service`, and an XDG autostart entry for the tray |
| Windows | `%LOCALAPPDATA%\Programs\Windowseat` | Task Scheduler logon tasks `Windowseat` and `Windowseat Tray` |
| macOS | `~/Library/Application Support/Windowseat/bin` | LaunchAgents `dev.windowseat.service` and `dev.windowseat.tray` |

On Linux the tray starts right away only if you ran `install` inside a graphical session. Otherwise it starts at your next graphical login. On Windows, `install` leaves your PATH alone and prints the folder to add if you want `windowseat` in a terminal.

`uninstall` also deletes the saved snapshot and the service socket. It never touches Claude, Codex or Copilot files.

To let agents read usage over MCP, register the server with each tool:

```sh
claude mcp add windowseat -- windowseat mcp
codex mcp add windowseat -- windowseat mcp
```

## License

[MIT](LICENSE)
