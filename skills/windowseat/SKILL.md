---
name: windowseat
description: Read remaining Claude Code, Codex and GitHub Copilot quota. Use when asked about usage, limits or reset times, or before starting long or parallel agent work.
---

# Reading quota with Windowseat

Windowseat reports what each provider says about its own limits. It reports numbers; the person decides what to do with them.

## Read

Prefer the MCP tools when the `windowseat` server is connected: `get_usage` for every provider, `get_provider_usage` for one. Otherwise run:

```sh
windowseat usage --json
```

Add `--provider claude|codex|copilot` for one provider. Add `--refresh` only when the person asks for a fresh read; the service refreshes on its own, and a refresh within 60 seconds of the last read returns the cached reading. Without a running service the command reads providers directly and says so on stderr. That takes a few seconds and is fine.

If `windowseat` is not installed, read [raw-reads.md](raw-reads.md).

The read is done when every provider in the output has been classified by the rules below.

## Interpret

Each entry in `providers[]` is one provider. Check these in order:

1. **`state`**. `healthy` and `partial` are current readings. `stale` keeps the last good meters and adds the newest `error`. `auth_required`, `provider_unavailable`, `malformed_response` and `unsupported_version` mean no usable reading: report the `error.message` and `error.recovery_hint`.
2. **`freshness.state`**. `current` is trustworthy. `stale` is older than `max_age_seconds`. `expired` means a reset time has passed, so the numbers describe a window that no longer exists.
3. **`usage_allowed`**, when present, is the provider's own yes or no. Report it as given.
4. **`meters[]`**, one per limit.

A meter or provider that is absent is unknown. Report it as unknown, never as zero.

## Meters

| Provider | Meter `id` | Value | Reset |
| --- | --- | --- | --- |
| claude | `session` | `used_percentage` of the five-hour session | `window.resets_at` |
| claude | `weekly_all` | `used_percentage` across all models | `window.resets_at` |
| claude | `weekly_<model>` | `used_percentage` for one model, named in `model` | `window.resets_at` |
| codex | `five_hour`, `seven_day` | `used_percentage`; window length in `window.duration_seconds` | `window.resets_at` |
| copilot | `premium_interactions` | `used` of `entitlement` requests, `remaining_percentage` left | unknown |
| copilot | `chat`, `completions` | usually `unlimited: true` | unknown |

Copilot readings are always `partial` because the runtime reports the read time in place of a reset time. That time sits in `window.raw_reset_at`; treat Copilot reset times as unknown.

`issues[]` lists fields that could not be read. Mention them when they touch a meter the person asked about.

## Report

Give the person, per provider: each meter's value, its reset time in their local time with time remaining, and any non-healthy state, non-current freshness, or `usage_allowed: false`. Keep it to one line per meter.
