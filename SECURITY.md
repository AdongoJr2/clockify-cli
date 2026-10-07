# Security Policy

## Supported Versions

Only the latest release is actively maintained. Always upgrade to the newest
version (`clockify --version`) before reporting a suspected vulnerability.

| Version | Supported          |
| ------- | ------------------ |
| latest  | :white_check_mark: |
| older   | :x:                |

## Reporting a Vulnerability

**Please do not open a public issue.** Instead, use GitHub's private
vulnerability reporting:

1. Go to the repository's **Security** tab → **Advisories** → **Report a
   vulnerability** (maintainers: this must be enabled under
   *Settings → Code security → Private vulnerability reporting*).
2. Describe the issue, the affected version (`clockify --version`), and — if
   possible — steps to reproduce or a minimal proof of concept.

You can expect an acknowledgement, and we will keep you informed as we assess
and fix the issue. Please give us reasonable time to release a fix before any
public disclosure.

## Scope

In scope: the CLI itself (`cmd/`, `pkg/clockify/`), its build/release
pipelines (`.github/workflows/`, `Dockerfile`), and the official container
images on GHCR. The demo mock server (`scripts/`) and test fixtures are
explicitly out of scope.

## A note for users

This tool handles your Clockify **API key** — a bearer credential. The CLI
never sends it anywhere except the configured Clockify API host (`X-Api-Key`
header). Verify the `base_url` / `CLOCKIFY_BASE_URL` you configure points at
a host you trust, keep your config file (`~/.config/clockify-cli/config.yaml`)
readable only by you (mode `0600`, enforced by `config init`), and revoke the
key in Clockify → Profile Settings → API if you suspect it leaked.
