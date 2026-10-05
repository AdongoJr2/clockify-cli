# clockify ⏱

[![CI](https://github.com/AdongoJr2/clockify-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/AdongoJr2/clockify-cli/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

A modern, colorful CLI for [Clockify](https://clockify.me) — start and stop
timers, log past time entries, and list recent activity without leaving your
terminal.

## Features

- 🕹️ Start/stop live timers from the command line
- 📝 Log past entries via flags (scriptable) or an interactive form
- 📋 Colorful, aligned table of your most recent entries
- 🎨 Interactive prompts ([huh](https://github.com/charmbracelet/huh)) and styled output ([lipgloss](https://github.com/charmbracelet/lipgloss))
- 🔐 API key stored locally with `0600` permissions — never hardcoded
- ⚡ Disk-cached user/project lookups to stay within Free-plan rate limits

## Installation

Requires [Go](https://go.dev/dl/) 1.27 or newer.

```bash
git clone https://github.com/AdongoJr2/clockify-cli.git
cd clockify-cli
go build -o clockify .
sudo mv clockify /usr/local/bin/   # optional
```

Or straight from the repo:

```bash
go install github.com/AdongoJr2/clockify-cli@latest
```

## Configuration

1. Create an API key in Clockify → **Profile Settings → API**.
2. Run the interactive setup:

```bash
clockify config init
```

This validates your key against the API, lets you pick a workspace, and saves
everything to `~/.config/clockify-cli/config.yaml` (mode `0600`). Config lives
under `$XDG_CONFIG_HOME` when that variable is set.

Alternatively, use environment variables (they take precedence over the file):

| Variable               | Purpose                                  |
| ---------------------- | ---------------------------------------- |
| `CLOCKIFY_API_KEY`     | API key from Profile Settings → API      |
| `CLOCKIFY_WORKSPACE_ID`| Workspace to track time in               |
| `CLOCKIFY_BASE_URL`    | Optional API host override (regional, e.g. `https://euc1.clockify.me/api/v1`) |

## Usage

```bash
clockify                       # help & version
clockify config init           # interactive setup

clockify start -d "Writing docs" -p "Website Redesign"
clockify stop

clockify log -d "Standup" --start "09:30" --duration 15m
clockify log -d "Deep work" --start "2026-10-05 14:00" --end "16:00" -p "Website Redesign"
clockify log                    # interactive form (project picker by name)

clockify list                   # last 5 entries
clockify list -n 10             # last 10 entries
```

### Time formats

Times are interpreted in your **local timezone** and stored as UTC:

| Input                    | Meaning                          |
| ------------------------ | -------------------------------- |
| `14:00`                  | today at 14:00 local             |
| `2026-10-05 14:00`       | that date/time local             |
| `2026-10-05T14:00`       | that date/time local             |
| `2026-10-05T14:00:00Z`   | RFC3339 (offset respected)       |
| `1h30m` (as `--duration`)| Go duration                      |

Projects can be passed by **name** (exact or unique substring) or by raw ID.
If a timer is already running, `clockify start` asks whether to stop it first
(non-interactive runs must call `clockify stop` explicitly).

## Rate limits

Clockify allows **30 API requests per hour per workspace on the Free plan**
(50 requests/second on paid plans). This CLI keeps usage low by caching your
user (24h) and project list (15 minutes) under
`${XDG_CACHE_HOME:-~/.cache}/clockify-cli/`. Delete that directory to force a
refresh.

## Testing

The project has three layers of testing — from fastest to most realistic:

### 1. Automated test suite (unit + end-to-end)

```bash
go test -race -cover ./...
```

- `pkg/clockify` — HTTP client unit tests against `httptest` servers: auth
  header, paths, query params, pagination, PUT-vs-PATCH semantics, UTC
  normalization, and error-envelope parsing (401/403/429…).
- `cmd` — time parsing, project resolution, caching, table rendering, plus
  **end-to-end tests that run the real Cobra commands** (`list`, `log`,
  `start`, `stop`, `config init`) against an in-process mock API and assert on
  both terminal output and the exact requests sent to the server.

Useful variants:

```bash
go test -v ./...               # verbose, per-test output
go test -run TestListEndToEnd ./cmd   # one test
go test -coverprofile=cover.out ./... && go tool cover -func=cover.out
```

### 2. Manual demo — no Clockify account needed

A bundled mock API server lets you click through the real binary:

```bash
# terminal 1
python3 scripts/mock_clockify.py          # listens on 127.0.0.1:8977

# terminal 2 (repo root)
export CLOCKIFY_API_KEY=mock-key
export CLOCKIFY_WORKSPACE_ID=w1
export CLOCKIFY_BASE_URL=http://127.0.0.1:8977

./clockify list
./clockify log -d "Demo work" --start 14:00 --duration 1h -p "Website Redesign"
./clockify start -d "Demo timer"
./clockify stop
```

To exercise the interactive forms, run `./clockify log` or `./clockify
config init` with **no flags** in a real terminal (Huh forms need a TTY).

### 3. Against real Clockify

1. Create an API key (Clockify → Profile Settings → API).
2. `./clockify config init` and pick your workspace.
3. Try `./clockify start -d "test"`, `./clockify stop`, `./clockify list`.

> **Free plan:** 30 API requests/hour/workspace — use the mock for heavy
> experimenting to avoid burning your quota.

## Releasing

Stamp the version at build time:

```bash
go build -ldflags "-X github.com/AdongoJr2/clockify-cli/cmd.version=v1.0.0" -o clockify .
```

Then tag and publish:

```bash
git tag v1.0.0 && git push --tags
gh release create v1.0.0 clockify --title "v1.0.0"
```

## License

[MIT](LICENSE)
