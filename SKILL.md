---
name: work
description: Run a customer's Work account — CRM (companies, contacts, deals), invoices, and projects/tasks — through the `work` CLI. Use whenever asked to create, list, update, or read anything in Work, or to onboard a customer's book of business into it.
---

# Work

Work is a small business tool to help you win the work, deliver it and get
paid: customers, delivery work, money. The
`work` CLI covers the whole public API — anything a person can do in the web
app, you can do here. The command table is generated from the API's
contract-tested OpenAPI spec, so what this skill describes is what the binary
does; `work help` is the ground truth at whatever version is installed, and
`work help <resource>` (no token needed) lists a resource's every verb and
writable flag — read it before guessing a flag name.

## Setup

```sh
curl -fsSL https://work.betterthangood.xyz/install-cli | bash
```

No runtime needed — one binary, macOS/Linux/Windows.

Authentication is a personal access token the customer creates in
Settings → API tokens. For any task that changes data, ask for
a **write** token; read suffices for reporting. Then:

```sh
work auth login            # prompts for the token, verifies it, saves it
echo "$TOKEN" | work auth login   # or pipe it, which is your case
```

There is no `--token` flag — a token in argv lands in shell history and in
every process list — so pipe it on stdin or set `WORK_TOKEN` in the
environment. `WORK_URL` overrides the host
(default `https://work.betterthangood.xyz`); `WORK_CONFIG` overrides the config path, so
two accounts can be held side by side.

## Output and exit codes

- Piped (your case), output is the API's `data` payload as pretty JSON on
  stdout. On a terminal it renders as a table; `--format json|table` forces.
- Money out is always integer **cents** (`value_cents`, `total_cents`) with
  a `currency` beside it. Money in follows the flag's name: a `-cents` flag
  takes cents (`work deals create --value-cents 500000` is $5,000). The flag
  name is the rule — never convert against it.
- Exit 0 success, 1 the server refused (stderr has `code: message` plus
  per-field detail lines on a 422), 2 the command itself was wrong (unknown
  verb, bad flag) — nothing was sent.
- A 422 detail names the API's column, which is not always the flag you set.
  Match it to the nearest flag, fix that, and resend.
- Deletes answer 204: success is silence plus exit 0.

## Commands

Grammar: `work <resource> <verb> [id] [--flags]`. A create or update takes
the resource's writable fields as flags; only flags you set are sent.

```sh
work contacts list [--page N] [--per-page N]     # every resource: list, show
work contacts show 12
work contacts create --first-name Ada --last-name Lovelace --email ada@example.com [--company-id 7]
work contacts update 12 --title "CTO"
work contacts delete 12                          # contacts, companies, deals, projects, tasks
```

The whole surface, with every writable flag (`work help <resource>` is
the ground truth at the installed version):

| Resource | Verbs | Writable flags |
| --- | --- | --- |
| `companies` | list, show, create, update, delete | `--name --domain --address` |
| `contacts` | list, show, create, update, delete | `--first-name --last-name --email --phone --title --company-id` |
| `deals` | list, show, create, update, delete | `--name --stage --value-cents --expected-close-on --company-id --contact-id --owner-id` |
| `projects` | list, show, create, update, delete | `--name --status --description --due-on --company-id --deal-id` |
| `tasks` | list, show, create, update, delete | `--title --status --description --due-on --project-id --assignee-id` |
| `invoices` | list, show | read-only — invoices are created by CSV import in the web app |

`projects` and `tasks` come from the Projects layer and answer 404 on an
account without it.

Moving a deal through the pipeline is an update of its stage:

```sh
work deals update 412 --stage won
```

### Invoices are read-only

Invoices are authored outside the API — a person imports them from a CSV in
the web app — so the CLI only lists and shows them:

```sh
work invoices list [--page N] [--per-page N]
work invoices show 88
```

## Conventions for agents

**Safe to run unattended:** every list and show, and `create`/`update` on
contacts, companies, deals, projects and tasks.

**Confirm with the person first:**

- Any `delete` — there is no undo.
- Anything while holding a write token on a large loop — the rate limit is
  1,000 requests/hour per token and every write lands in the account's
  audit log under your name.

**Working well:**

- Prefer ids you just saw (`show`, or the JSON a create answered with) over
  guessing; another account's id is indistinguishable from a missing record
  (404 either way, deliberately).
- Lists are paginated at 25, ceiling 100 — `--page N --per-page N`. There is
  no next-page marker: a page shorter than `per_page` (or empty) is the end.
  To read everything, walk `--page 1, 2, …` until a short page.
- A 422's detail lines name the field and the problem; fix the flag, don't
  retry unchanged. A 429 means slow down, not switch resources — the budget
  is shared.
- An authentication error (`unauthorized` on stderr) means the token is
  absent, revoked, or expired — re-run `work auth login` with a fresh token
  from the customer, or fix `WORK_TOKEN`; retrying without that changes
  nothing. A `forbidden` on a write means the token is read-scoped: ask for
  a write token.

## Onboarding a customer

The whole book of business, no UI:

```sh
work companies create --name "Lumen Health" --domain lumenhealth.com
work contacts create --first-name Priya --last-name Raman \
  --email priya@lumenhealth.com --company-id 13
work deals create --name "Lumen rebranding" --stage lead \
  --value-cents 500000 --company-id 13
# …the deal closes…
work deals update 412 --stage won
```

If the customer has projects on: `work projects create --name … --company-id 13`,
then `work tasks create --title … --project-id 4`.
