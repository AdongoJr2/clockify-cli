# clockify ⏱

[![CI](https://github.com/AdongoJr2/clockify-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/AdongoJr2/clockify-cli/actions/workflows/ci.yml)
[![Release](https://img.shields.io/badge/release-latest-03A9F4?logo=github&logoColor=white)](https://github.com/AdongoJr2/clockify-cli/releases/latest)
[![Container](https://img.shields.io/badge/container-ghcr.io-03A9F4?logo=docker&logoColor=white)](https://github.com/AdongoJr2/clockify-cli/pkgs/container/clockify-cli)
[![Go](https://img.shields.io/badge/go-1.27-03A9F4?logo=go&logoColor=white)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

A modern, colorful CLI for [Clockify](https://clockify.me) — start and stop
timers, log past time entries, and list recent activity without leaving your
terminal.

## Features

- 🕹️ Start/stop live timers from the command line
- 📝 Log past entries via flags (scriptable) or an interactive form
- ✏️ Update existing entries (description, times, project, billable)
- 🗑️ Delete entries by ID or description (with confirmation)
- 📋 Colorful, aligned table of your most recent entries
- 🎨 Interactive prompts ([huh](https://github.com/charmbracelet/huh)) and styled output ([lipgloss](https://github.com/charmbracelet/lipgloss))
- ⏳ Animated spinner while API requests are in flight (TTY only — pipes stay clean)
- 🔐 API key stored locally with `0600` permissions — never hardcoded
- ⚡ Disk-cached user/project lookups to stay within Free-plan rate limits

## Installation

### Prebuilt binaries (all platforms)

Grab a tarball from [Releases](https://github.com/AdongoJr2/clockify-cli/releases/latest):

```bash
# Linux (amd64)
curl -fsSL -o clockify.tar.gz \
  https://github.com/AdongoJr2/clockify-cli/releases/latest/download/clockify-linux-amd64.tar.gz
tar -xzf clockify.tar.gz && sudo mv clockify /usr/local/bin/

# macOS (Apple silicon) — use darwin-arm64, or darwin-amd64 for Intel
curl -fsSL https://github.com/AdongoJr2/clockify-cli/releases/latest/download/clockify-darwin-arm64.tar.gz \
  | tar -xz && sudo mv clockify /usr/local/bin/
```

Checksums (`SHA256SUMS`) are published with every release; verify with
`sha256sum -c SHA256SUMS`.

### Container

```bash
docker pull ghcr.io/adongojr2/clockify-cli:latest

# keep config/cached lookups in a named volume so credentials persist
docker volume create clockify-config
docker run --rm -it -v clockify-config:/config ghcr.io/adongojr2/clockify-cli:latest config init
docker run --rm -it -v clockify-config:/config ghcr.io/adongojr2/clockify-cli:latest list
```

Images are multi-arch (`linux/amd64`, `linux/arm64`) and versioned by git tag
(`ghcr.io/adongojr2/clockify-cli:v0.5.0`) plus `latest`.

### From source

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
clockify list --range last-week # only last week
clockify list --from 2026-10-01 --to 2026-10-07
clockify list --sort duration    # longest first
clockify list --sort start --asc # oldest first
clockify list --no-trunc        # full descriptions and project names

clockify update <entry-id> -d "Renamed task"
clockify update <entry-id> --start "09:00" --end "11:00"
clockify update <entry-id> --start "14:00" --duration 2h -p "Backend API"
clockify update                 # pick a recent entry, edit via interactive form

clockify projects                          # active + archived projects
clockify projects --active                 # hide archived
clockify projects --archived               # archived only
clockify projects -p "Website"             # server-side name filter
clockify projects -p "Website" --exact     # exact name match

clockify duplicate                 # "do it again": pick a recent entry, copy it
clockify duplicate <entry-id>      # copy a specific entry, keeps everything

clockify delete -d "Testing..."  # find by description, then confirm
clockify delete <entry-id>        # by ID, with confirmation
clockify delete --yes -d "..."    # no prompt (scripts/CI)
clockify delete                   # pick from recent entries
```

### Batch logging (YAML)

Log many entries at once from a YAML file — Clockify's API has no batch-create
endpoint, so this posts one request per entry with bounded concurrency:

```bash
clockify log --batch entries.yaml              # validate everything, then log
clockify log --batch entries.yaml --dry-run    # validate + print the plan, send nothing
clockify log --batch - < entries.yaml          # read from stdin
```

```yaml
default-project: Website Redesign   # optional; used by entries without a project
entries:
  - description: Standup
    start: "09:30"                 # "14:00", "2026-10-05 14:00", or RFC3339
    duration: 15m                   # ... or end: "09:45" (never both)
    project: Backend API            # optional; project name or ID
    billable: false                 # optional; omit to use the project default
```

A ready-to-edit example lives in [`examples/entries.yaml`](examples/entries.yaml).

**Validation happens first — nothing is sent if anything is wrong:**

- All problems are reported together, numbered by entry
  (`entry 3 ("Deep work"): description is required`)
- Unknown/misspelled keys are rejected (catches `duraton:` typos)
- Each entry needs a description, a start, and exactly one of end/duration
- Project names are resolved **before** the first write; an unknown project aborts
  the whole batch ("no entries were logged")
- `--dry-run` needs no API credentials at all — it just validates and prints a plan

**Execution:**

- `--concurrency N` (default 3, max 10) controls parallelism
- Failures don't abort the batch: every entry is attempted, then failures are
  listed individually and the command exits non-zero if any failed
- Cost is one request per entry (Free plan: 30/hour, so batches are budget-aware)

### Filtering by date range

`clockify list` can filter entries by a predefined period or a custom window:

```bash
clockify list --range today          # or: yesterday, this-week (week),
                                     #    last-week, this-month (month),
                                     #    last-month, 7d, 30d, 90d, all
clockify list --from 2026-10-01 --to 2026-10-07
clockify list --from "2026-10-01 09:00"      # --to defaults to now
clockify list --range last-week -n 10        # combine with -n
```

- Weeks are **Monday-based (ISO)**; all bounds are computed in your local
  timezone and sent to Clockify as UTC.
- Filtering matches an entry's **start time**, so an entry that began before
  the window but ran into it won't be listed (that's how the Clockify API works).
- Filtered views show up to 50 entries by default (a full page) instead of 5;
  pass `-n` to cap it.
- Long descriptions and project names are truncated to fit the table
  (multi-line descriptions collapse to a single row); pass `--no-trunc` to
  show them in full.
- `--sort` orders entries client-side (the API has no sort parameter) by
  `start`, `end`, `duration`, `description`, or `project`. Times/durations
  default to most-recent/longest first, text to A→Z; override with
  `--asc`/`--desc`. A sorted view fetches a full page (50) unless `-n` is
  given, so ordering applies to more than the latest few entries.
- With shell completion enabled (`clockify completion bash`), `--range`
  suggests the valid presets.

### Projects

`clockify projects` lists the workspace's projects (name, ID, active/archived
status, billable) — useful for seeing what's available before `log`ging or
`start`ing against one:

```bash
clockify projects                          # active + archived
clockify projects --active                 # hide archived
clockify projects --archived               # archived only
clockify projects -p "Website"             # server-side substring filter
clockify projects -p "Website" --exact    # exact-name match
clockify projects --active -n 5
```

Only `--archived` filters server-side; `--active` is screened client-side
(the Clockify API has no "active only" filter). `-p` runs the same
server-side name search you can rely on when picking projects by name —
use `--exact` when you need an unambiguous match.

### Deleting entries

`clockify delete` targets an entry by **ID**, by **description** (`-d`, exact
match preferred over substring), or by picking from your recent entries in a
terminal. Deletion is permanent, so the CLI always shows which entry will be
removed and asks you to confirm — pass `--yes` to skip the prompt in
non-interactive runs.

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

Releases are automated — push a tag and GitHub Actions does the rest:

```bash
git tag v0.5.0
git push origin v0.5.0
```

That triggers `.github/workflows/release.yml`, which:

1. cross-compiles binaries for `linux/amd64`, `linux/arm64`, `darwin/amd64`,
   `darwin/arm64`, `windows/amd64` (static, version stamped from the tag),
2. packs each into `.tar.gz` / `.zip` and uploads them as workflow artifacts,
3. generates `SHA256SUMS`,
4. creates the GitHub Release with auto-generated release notes and attaches
   every archive.

The same tag also publishes a versioned container image to
`ghcr.io/adongojr2/clockify-cli:v0.5.0` via `.github/workflows/docker.yml`.

To re-publish an existing tag, run the **Release** workflow manually
(*Actions → Release → Run workflow*) and pass the tag.

Day-to-day pushes to `main` run **CI** (gofmt, vet, build, `go test -race`) and
publish a `latest` container image.

## Changelog

Release history lives in **[CHANGELOG.md](CHANGELOG.md)** ([Keep a Changelog](https://keepachangelog.com/en/1.1.0/) + [SemVer](https://semver.org/)). GitHub release notes are generated from that file, so the Releases page and the changelog never drift apart — add your entry under `## [Unreleased]` and it becomes the notes for the next tag.

## License

[MIT](LICENSE)
