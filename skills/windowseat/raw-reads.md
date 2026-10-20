# Raw provider reads

Use these only when `windowseat` is not installed. Each command asks the provider's own CLI and spends no quota. Read nothing else: credential files, keychains and tokens stay closed.

## Claude Code

```sh
env -u CLAUDECODE -u CLAUDE_CODE_ENTRYPOINT claude -p /usage --output-format stream-json --verbose --no-session-persistence
```

Find the line with `"type":"assistant"`. Its `usage_report.rate_limits.limits[]` holds one row per meter: `kind` (`session`, `weekly_all`, `weekly_scoped`), `percent` used, `resets_at`, and for `weekly_scoped` the model in `scope.model.display_name`. The final `"type":"result"` line must show `num_turns` 0 and `total_cost_usd` 0; anything else means the read spent quota, so stop and tell the person.

## Codex

```sh
(printf '%s\n' '{"id":1,"method":"initialize","params":{"clientInfo":{"name":"windowseat","version":"0.1.0"},"capabilities":null}}' '{"id":2,"method":"account/rateLimits/read","params":{"excludeResetCreditDetails":true}}'; sleep 10) | timeout 15 codex -s read-only -a never app-server
```

Send exactly these two requests. Take the line with `"id":2`. In `result.rateLimits`, `primary` and `secondary` each carry `usedPercent`, `windowDurationMins` and `resetsAt` (epoch seconds). Name each window by its duration: 300 minutes is the five-hour window, 10080 is the weekly one. A null window is unknown. `result.ordinaryUsageAllowed` is the provider's own yes or no. Leave `accountId` out of anything you report.

## GitHub Copilot

Copilot has no one-line read. Install Windowseat, or tell the person to check the quota line in the Copilot CLI footer.
