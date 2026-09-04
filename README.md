# The work CLI

The CLI for [Work](https://work.betterthangood.xyz), the
small business tool that helps you win the work, deliver it and get paid.
One Go binary covering the whole public API: CRM (companies, contacts,
deals), invoicing and payments, projects and tasks. Anything a person can
do in the web app, you (or your agent) can do here.

Full documentation: [docs.betterthangood.xyz/work/cli](https://docs.betterthangood.xyz/work/cli).

## Install

```sh
curl -fsSL https://work.betterthangood.xyz/install-cli | bash
```

Works on macOS/Linux/Windows. The script detects the
platform, downloads the matching archive from this repository's
[releases](https://github.com/betterthangoodxyz/work-cli/releases), verifies
its checksum against `checksums.txt` before anything touches the PATH, and
installs into `/usr/local/bin` (or `~/.local/bin`). `WORK_INSTALL_DIR` picks
the directory, `WORK_VERSION=1.0.0` pins a release, and `WORK_RELEASES_BASE`
replaces the download location outright.

`checksums.txt` is written in the format `shasum -a 256 -c` reads, so a
mirror — or a suspicious human — can verify a downloaded archive with the
standard tool rather than trusting the script's own comparison.

## Auth

A personal access token from Settings → API tokens in your Work account —
`read` for looking, `write` for changing. Then:

```sh
work auth login                     # prompts, verifies the token, saves it
pass work/token | work auth login   # or read it from stdin
```

There is no `--token` flag — a token in argv lands in shell history and in
every process list. `WORK_TOKEN` beats the saved config; `--base-url`,
`WORK_URL`, saved config, then `https://work.betterthangood.xyz`. The config
lives at `~/.config/work/config.json` mode 0600; `WORK_CONFIG` points
elsewhere.

## Usage

```sh
work <resource> <verb> [id] [--flags]
```

`work help` prints the whole surface; `work help <resource>` lists a
resource's every verb and flag. Help never needs a token.

| Resource | Verbs |
| --- | --- |
| `companies`, `contacts`, `deals`, `projects`, `tasks` | `list`, `show`, `create`, `update`, `delete` |
| `invoices` | `list`, `show`, `create`, plus `send` and `pay` — no update or delete; a sent invoice is a record |
| `payments` | `list`, `show` — read-only; money in goes through `work invoices pay` |

```sh
work contacts list
work deals create --name "Lumen rebranding" --stage lead --value-cents 500000
work invoices send 88
```

Output is a table on a terminal, pretty JSON when piped (`--format` forces).
Exit codes: 0 ok, 1 the server refused, 2 the command was wrong and nothing
was sent.

## Agents

`work` works with any AI agent that can run shell commands.
[`SKILL.md`](SKILL.md) is the agent skill: point your agent at it for the
whole workflow, including which commands are safe unattended and which
always confirm first.

## How it's built

The command table (`generated.go`) is generated from the API's
contract-tested OpenAPI spec (`docs/openapi.json`) — the CLI's verbs, flags
and action paths all derive from it, so a client generated from the spec
cannot drift from the server. `generated_test.go` independently re-derives
the table from the spec, so a hand edit fails the suite. The rest — the
HTTP envelope, output formatting, auth — is hand-written and thin by
design: no business logic, stdlib only.

See [AGENTS.md](AGENTS.md) for how the spec, the table and releases are
kept in sync with the Work app.

## Development

```sh
go test ./...    # the suite, including the spec contract test
go build -o work .
bin/quality      # vet, gofmt, staticcheck, complexity, duplication, coverage
```

## Releasing

Tag a version and push it; the release workflow builds the archives and
attaches them with `checksums.txt`:

```sh
git tag v1.0.1 && git push origin v1.0.1
```

`bin/build 1.0.1` produces the same artifacts locally into the gitignored
`dist/`.

## License

[MIT](LICENSE)
