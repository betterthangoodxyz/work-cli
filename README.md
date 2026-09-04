# Work CLI

`work` is the official command-line interface for Work. Manage companies, contacts, deals, invoices, projects and tasks from your terminal or through AI agents.

## Install on Mac/Linux/Windows

```sh
curl -fsSL https://work.betterthangood.xyz/install-cli | bash
```

Full documentation: [docs.betterthangood.xyz/work/cli](https://docs.betterthangood.xyz/work/cli).

Installs into `/usr/local/bin` (or `~/.local/bin`). `WORK_INSTALL_DIR` picks
the directory, `WORK_VERSION=1.0.0` pins a release, and, optionally, `WORK_RELEASES_BASE`
replaces the download location.

At a terminal, `work` checks for a newer release at most once a day (never
when piped, never blocking a command) and prints a one-line notice on
stderr when one exists. `WORK_NO_UPDATE_CHECK=1` turns it off.

## Auth

Grab your personal access token from Settings → API tokens in your Work account —
`read` for looking, `write` for changing. Then:

```sh
work auth login
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
| `invoices` | `list`, `show` — read-only; invoices are authored by CSV import in the web app |

```sh
work contacts list
work deals create --name "Lumen rebranding" --stage lead --value-cents 500000
work invoices show 88
```

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
