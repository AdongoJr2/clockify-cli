# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Release notes on GitHub are generated from the sections below, so keep them
concise and user-facing.

## [Unreleased]

### Fixed

- Release workflow: check out the repository before creating the release, so
  `gh release create --verify-tag` can resolve the tag with git.
- README badges: switched the release badge to a static `release-latest` badge
  (shields.io cannot query private repos, so the dynamic version badge always
  rendered red) and fixed the container badge URL encoding.
- Upgraded every GitHub Actions workflow to Node 24 runtimes
  (`checkout@v7`, `setup-go@v7`, `upload-artifact@v7`, `download-artifact@v8`,
  `docker/*` latest) with `ACTIONS_RUNNER_FORCE_ACTIONS_NODE_VERSION: node24`.

## [0.5.0] - 2026-10-05

### Added

- **Batch logging**: `clockify log --batch <file|->` logs many entries from a YAML
  file (one request per entry, since the API has no batch-create endpoint).
  - The whole file is validated **before any request is sent**: structure,
    unknown keys, required fields, `end`/`duration` exclusivity and time parsing;
    all problems are reported together and nothing is sent if anything is wrong
  - `--dry-run` prints the normalized plan without needing credentials
  - `--concurrency N` (1–10, default 3) bounds parallelism
  - Project names are resolved once before the first write; an unknown project
    aborts the batch instead of logging half of it
  - Partial failures are reported per entry; the command exits non-zero
  - Schema: `description`, `start`, `end` *or* `duration`, `project`, `billable`,
    plus an optional top-level `default-project`
  - Example file: `examples/entries.yaml`
- Automated GitHub Releases: cross-compiled binaries for `linux/amd64`,
  `linux/arm64`, `darwin/amd64`, `darwin-arm64` and `windows/amd64`, packaged
  with `SHA256SUMS`, attached to a release created from a pushed `v*` tag.
- Container publishing: multi-arch (`linux/amd64`, `linux/arm64`) images at
  `ghcr.io/adongojr2/clockify-cli`, tagged `latest` and per version.

### Changed

- GitHub Actions upgraded to Node 24 runtimes (`checkout@v7`, `setup-go@v7`,
  `upload-artifact@v7`, `download-artifact@v8`, `docker/*@v4`+).

### Fixed

- Disk cache no longer self-invalidates: freshly written caches keep the API key
  hash, so user/project lookups are actually reused between commands.
- `renderTable` renders nothing when there are no rows instead of a lone header.

## [0.4.0] - 2026-10-05

### Added

- Date filtering for `clockify list`:
  - `--range` presets: `today`, `yesterday`, `this-week` (`week`), `last-week`,
    `this-month` (`month`), `last-month`, `7d`, `30d`, `90d`, `all`
  - `--from` / `--to` for custom windows (same time formats as `--start`)
  - ISO (Monday-based) weeks, computed locally and sent as UTC
  - The table header names the active range; filtered views show a full page
    (50) unless `-n` is given
  - Shell completion for `--range`

## [0.3.0] - 2026-10-05

### Added

- `clockify delete [entry-id]` to remove time entries:
  - target by ID, by description (`-d`, exact match preferred over substring),
    or pick from your recent entries in a terminal
  - always shows which entry will be removed and asks for confirmation
    (`--yes` to skip in scripts)
  - API: `DELETE /workspaces/{ws}/time-entries/{id}` (204 No Content)

## [0.2.0] - 2026-10-05

### Added

- `clockify update [entry-id]` to edit an existing entry (description, start/end
  time, project, billable) with merge semantics — only what you change is
  updated. Targets by ID, by picking from recent entries, or via a form
  preloaded with the entry's current values.
- Animated braille spinners while API requests are in flight (TTY only, so
  pipes and CI output stay clean; fast responses never flash).
- API: `GET /workspaces/{ws}/time-entries/{id}` used as the update merge base.

## [0.1.0] - 2026-10-05

### Added

- Initial release.
- Commands: `config init`, `start`, `stop`, `log`, `list`.
- Clockify REST client (GET/POST/PATCH/PUT) with typed errors for 401/403/404/429.
- Interactive forms ([huh](https://github.com/charmbracelet/huh)) and styled
  output ([lipgloss](https://github.com/charmbracelet/lipgloss)).
- Credentials from `CLOCKIFY_API_KEY` / `CLOCKIFY_WORKSPACE_ID` or
  `~/.config/clockify-cli/config.yaml` (mode `0600`).
- Disk-cached user/project lookups to respect the Free plan's 30 requests/hour.
- CI workflow (gofmt, vet, build, `go test -race`).