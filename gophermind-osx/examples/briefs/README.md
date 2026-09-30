# Example briefs

Two kinds of example live here. Use the short ones to try the interview, and the
longer ones to see what a complete brief looks like.

## Starters (01 to 03): short and incomplete on purpose

A paragraph or two each. Pick one, click **Start Breakdown** in the Pipeline
panel, and the model asks you questions to fill the gaps before it writes
`SPEC.md`, `ROADMAP.md`, and `assignments.json` into a `.planning/` directory.
These files have no special format: **Pick Brief...** reads any plain-text file.

- `01-cli-tool.md`: a small command-line utility
- `02-rest-api.md`: a small backend service
- `03-menu-bar-app.md`: a small native macOS utility

## Complete briefs (04 to 08): the v2 brief format

Each one starts with a YAML block that says how the run should behave, followed by
seven required sections: Overview, Features, Architecture, Data, Constraints, Out of
scope, and Acceptance. Check any of them (or your own) with:

```sh
gophermind brief validate 06-hookrelay.md
```

An invalid brief exits with code 2 and names the field. All five here validate
with no warnings. Every brief in this format is for a **Go** project (the only
language v2 accepts today), and none uses a real account, key, or address.

They are ordered by size, and between them they use every option the format has:

| Brief | What it builds | Size | Landing | On unclear | Approval between waves | Secrets | Env settings | Hosts | Revisions |
|---|---|---|---|---|---|---|---|---|---|
| `04-csvstat` | command-line CSV summarizer | 2 features | `diff_only` | `assume_and_document` | off | 0 | 1, with a default | 2, module downloads | 0 |
| `05-retryx` | retry library, no program at all | 3 features | `commit` on its own branch | `halt` | off, set explicitly | 0 | 0 | 2 | 1 |
| `06-hookrelay` | signed-webhook relay service | 4 features | `pull_request` | `halt` | on | 2 | 4, all with defaults | 3, one allowed to fail | 2 |
| `07-ledger-sync` | batch data pipeline into SQLite | 5 features | `commit` | `assume_and_document` | off | 1 | 4, all with defaults | 4, one wildcard host allowed to fail | 3 |
| `08-taskboard` | API, live updates, background worker, and a CLI client | 7 features | `pull_request` on its own branch | `halt` | on | 3 | 8, one with no default | 3, a wildcard host allowed to fail | 2 |

What the options mean, in the order they appear in the block:

- `landing`: `diff_only` stops at a patch, `commit` commits each verified task, `pull_request` pushes a branch and opens one.
- `on_ambiguity`: `halt` stops and asks; `assume_and_document` picks the conservative option and records it on the task.
- `milestone_approvals`: pause for a human after every wave of work.
- `work_branch`: the branch to commit on; it defaults to `gm/<id>`.
- `secrets`: names only, never values. The harness asks for each one and keeps it in its vault.
- `env`: ordinary settings the program reads, with an optional default. A name cannot be both a secret and a setting.
- `network`: the hosts the build may reach; `critical: true` fails the task when a request fails, and `*.example.com` matches a whole domain.
- `budget`: the largest prompt for one task, and how many rewrite rounds a failed task gets.

A test (`gophermind-lib/briefv2/brief/examples_test.go`) parses every complete
brief here and fails if one stops validating or if the set stops covering an option.
