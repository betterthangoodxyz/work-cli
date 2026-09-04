# AGENTS.md

Guidance for agents and developers working in this repository. The product
context lives in the private app repo (`betterthangoodxyz/work`); this repo
is the public home of its CLI.

## What this is

The `work` CLI: one Go binary, stdlib only, generated from the Work public
API's OpenAPI spec. It is a thin client — a generated command table plus
auth, HTTP and output formatting. **It never contains business logic**;
behavior belongs in the API, and the CLI's job is to expose it faithfully.

- `generated.go` — the command table, generated from `docs/openapi.json`
  by the app repo's `bin/docs`. **Never edit by hand**; `generated_test.go`
  re-derives the table from the spec and fails on a hand edit.
- `docs/openapi.json` — a committed copy of the app's contract-tested
  spec. The app repo is its source of truth.
- `SKILL.md` — the agent skill, kept in step with the API surface by the
  app repo's `bin/docs --check`.
- `install-cli` — the install script, also served by the app at
  `https://work.betterthangood.xyz/install-cli`. The app repo's copy
  (`public/install-cli`) is the one customers download; keep the two
  identical.

## How the two repos stay in sync

The API is the contract, and it changes in the app repo first:

1. An API change lands in the app repo. Its `bin/docs` regenerates
   `docs/openapi.json`, `cli/generated.go` equivalents, and its
   contract test proves the spec against the running server.
2. The regenerated `openapi.json`, `generated.go` and any `SKILL.md`
   prose changes are copied here in one commit (the app repo's
   `docs/runbooks/cli-release.md` walks through it).
3. `go test ./...` here proves the table against the committed spec —
   the second lock. A stale copy fails the suite.
4. Tag a version; the release workflow builds and publishes.

A PR here that changes `generated.go` or `docs/openapi.json` without a
matching app-repo change is wrong by construction — close it and start
from the API.

## Quality bar

`bin/quality` is the gate, run by CI on every push and PR: `go vet`,
gofmt, staticcheck (including dead code), a complexity ceiling of 20,
duplication over 75 tokens, `go mod tidy -diff`, and a 75% coverage
floor. Match the app repo's house style: small well-named functions,
comments only for the "why", no new dependencies — stdlib is the rule,
and a PR adding a module needs a reason the standard library cannot
answer.

## Releasing

```sh
git tag v1.0.1 && git push origin v1.0.1
```

`.github/workflows/release.yml` runs the tests, cross-compiles
macOS/Linux/Windows archives with `bin/build`, and attaches them plus
`checksums.txt` to the GitHub release. The install script downloads from
`releases/latest/download/…`, so publishing the release is what ships.
The version is stamped at build time (`work version` answers it) and the
tag is the version: `v1.0.1` builds `1.0.1`.
