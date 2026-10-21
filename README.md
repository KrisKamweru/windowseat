# WindowSeat

WindowSeat shows how much quota you have left on Claude Code, Codex and GitHub Copilot. It asks each provider's own CLI for the numbers and reports them to you, to scripts and to coding agents.

It only reads. It never decides whether an agent should keep working, never switches accounts and never spends a reset credit.

## Why this exists

Claude Code, Codex and Copilot each track their own limits. Checking them separately is a chore, and coding agents need a way to read those limits too. Knowing how much quota remains helps you judge whether to start a long task.

Other monitors already solve parts of this. CodexBar has a mature macOS menu bar and a usage CLI. agent-usage-monitor exposes quota through MCP. But none of the reviewed projects combined a shared service, MCP access and a tray on Windows and Linux while keeping provider credentials out of the monitor. Several read OAuth tokens to fetch usage. Others rely on local transcripts, which alone cannot tell you how much account quota remains. A quota display should not need your token.

WindowSeat asks each provider's own CLI for its quota numbers. The provider CLI handles sign-in. WindowSeat never opens credential files or keychains. A local service caches the readings for the CLI, MCP server and tray, so each client doesn't need its own provider poll. When a read fails, WindowSeat marks the last good reading as stale instead of pretending usage is zero.

WindowSeat exists to give people and agents the same view of the quota left. You and your agents decide what to do with that information.

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

WindowSeat never opens credential files, keychains or tokens. Each provider CLI gets the environment unchanged and handles its own sign-in. The Codex and Copilot adapters only send methods on a fixed allowlist, so they can't reach the methods that spend reset credits or return credentials.

If a read fails, WindowSeat reports the failure and keeps the last good reading marked as stale. It never shows a failed read as 0%.

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

Run `git config core.hooksPath .githooks` once after cloning. The pre-commit hook blocks commits to `main` and runs `gofmt`, `go vet` and `go test` first.

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
.\windowseat.exe install --add-to-path
& "$env:LOCALAPPDATA\Programs\WindowSeat\windowseat.exe" uninstall
```

Running `install` again upgrades in place. It stops the old copies before it replaces them.

| Platform | Binaries | Starts at login through |
| --- | --- | --- |
| Linux | `~/.local/bin` | `systemd --user` unit `windowseat.service`, and an XDG autostart entry for the tray |
| Windows | `%LOCALAPPDATA%\Programs\WindowSeat` | Task Scheduler logon tasks `WindowSeat` and `WindowSeat Tray` |
| macOS | `~/Library/Application Support/WindowSeat/bin` | LaunchAgents `dev.windowseat.service` and `dev.windowseat.tray` |

On Linux the tray starts right away only if you ran `install` inside a graphical session. Otherwise it starts at your next graphical login. On Windows, add `--add-to-path` to put the install folder on your user PATH, so `windowseat` works in any new terminal. `uninstall` takes it off again. To install somewhere other than `%LOCALAPPDATA%\Programs\WindowSeat`, pass `--dir` with a full path, and pass the same `--dir` to `uninstall`.

`uninstall` also deletes the saved snapshot and the service socket. It never touches Claude, Codex or Copilot files.

To let agents read usage over MCP, register the server with each tool:

```sh
claude mcp add windowseat -- windowseat mcp
codex mcp add windowseat -- windowseat mcp
```

### Setup wizard on Windows

Each release on the [Releases page](https://github.com/KrisKamweru/windowseat/releases) has a `windowseat-<version>-windows-amd64-setup.exe`, and an `arm64` one for ARM PCs. It installs for your user only and needs no admin rights. You can pick the install folder, which defaults to `%LOCALAPPDATA%\Programs\WindowSeat`. Two checkboxes, both on by default, choose whether to show the tray icon and whether to add WindowSeat to your PATH. Running a newer setup upgrades in place. To remove WindowSeat, uninstall it from Apps & Features in Settings.

## License

[MIT](LICENSE)
