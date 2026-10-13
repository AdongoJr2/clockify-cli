# Contributing to clockify-cli

Thanks for stopping by — issues, ideas, and pull requests are all welcome.

## Ground rules

- Be kind and constructive (see [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)).
- One focused change per pull request.
- New features and bug fixes should come with tests (`go test -race ./...`).
- Reported a security problem? Read [SECURITY.md](SECURITY.md) first — please
  don't open a public issue for it.

## Development setup

Requires [Go](https://go.dev/dl/) 1.27 or newer. CI runs
[golangci-lint](https://golangci-lint.run/) (config in `.golangci.yml`) — worth
running locally too:

```bash
git clone https://github.com/AdongoJr2/clockify-cli.git
cd clockify-cli
go build ./...     # compile everything
go vet ./...       # static analysis
gofmt -l .         # must print nothing
go test -race ./...  # full suite (unit + end-to-end, no network needed)

# same checks as the CI lint job:
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
golangci-lint run
```

### Trying the binary without a Clockify account

```bash
python3 scripts/mock_clockify.py &   # fake API on 127.0.0.1:8977
export CLOCKIFY_API_KEY=mock CLOCKIFY_WORKSPACE_ID=w1 \
       CLOCKIFY_BASE_URL=http://127.0.0.1:8977
./clockify list
```

See the [Testing section of the README](README.md#testing) for the full story.

## Commit messages

Use [Conventional Commits](https://www.conventionalcommits.org/): `feat:`,
`fix:`, `docs:`, `test:`, `chore:`, `ci:`, `refactor:` — e.g.
`feat: add clockify projects command`. A clear body beats a clever title.

## Pull request checklist

- [ ] `gofmt -l .` prints nothing; `go vet ./...`, `golangci-lint run` and
      `go test -race ./...` pass
- [ ] New behavior has tests (see `cmd/*_test.go` and the in-process mock in
      `cmd/mock_test.go` for the established patterns)
- [ ] `README.md` and `CHANGELOG.md` updated (feature changes go under
      `## [Unreleased]`; the release workflow turns those sections into
      GitHub release notes)
- [ ] No secrets, tokens, or personal data committed (run
      `git diff --check` and re-read your diff)

## Release process (maintainers)

Releases are automated: update `CHANGELOG.md`, then tag and push —

```bash
git tag v0.9.0 && git push origin v0.9.0
```

— and the Release workflow builds binaries, creates the GitHub release from
the matching changelog section, and publishes the container image.
