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

## License

[MIT](LICENSE)
