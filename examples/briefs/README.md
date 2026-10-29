# Example briefs

Eight complete briefs, each a working example of the brief format. Every one is
for a **Go** project (the only language the loader accepts today), validates with
no warnings, and uses no real account, key or address. The full format rules are
in `gophermind-lib/serve/assets/brief-spec.md`, which the server also serves at
`/app/brief-spec.md`.

## Try one

1. Pick a brief. `04-csvstat` is the smallest to plan; `01-linkcheck` and `03-cliphist` are a good first run.
2. Check it: `gophermind brief validate examples/briefs/01-linkcheck.md`. An invalid brief exits with code 2 and names the field.
3. Upload it on the web page (`/app/briefs/new`). The briefs carry no `repo` line, because the server builds each run in a repository of its own.
4. To plan on your own machine instead, add `repo: /path/to/an-empty-git-repo` to the frontmatter and run `gophermind brief plan <file>`. Only a local plan reads that line.

Change the `id` to a free one before uploading: a second upload with the same id is refused.

## The set

| Brief | What it builds | Features | Landing | On unclear | Approval between waves | Secrets | Env settings | Hosts | Revisions |
|---|---|---|---|---|---|---|---|---|---|
| `01-linkcheck` | link checker for Markdown and HTML | 5 | `commit` | `assume_and_document` | off | 0 | 0 | 2, module downloads | default |
| `02-notes-api` | in-memory notes REST service | 6 | `pull_request` on its own branch | `halt` | on | 0 | 1, with a default | 0 | default |
| `03-cliphist` | clipboard history store and CLI | 5 | `diff_only` | `assume_and_document` | off | 0 | 2, with defaults | 0 | default |
| `04-csvstat` | command-line CSV summarizer | 2 | `diff_only` | `assume_and_document` | off | 0 | 1, with a default | 2 | 0 |
| `05-retryx` | retry library, no program at all | 3 | `commit` on its own branch | `halt` | off, set explicitly | 0 | 0 | 2 | 1 |
| `06-hookrelay` | signed-webhook relay service | 4 | `pull_request` | `halt` | on | 2 | 4, all with defaults | 3, one allowed to fail | 2 |
| `07-ledger-sync` | batch data pipeline into SQLite | 5 | `commit` | `assume_and_document` | off | 1 | 4, all with defaults | 4, one wildcard host allowed to fail | 3 |
| `08-taskboard` | API, live updates, background worker, and a CLI client | 7 | `pull_request` on its own branch | `halt` | on | 3 | 8, one with no default | 3, a wildcard host allowed to fail | 2 |

Between them they use every option the format has. A test
(`gophermind-lib/build/brief/examples_test.go`) parses every brief here and fails
if one stops validating or if the set stops covering an option.

## What the options mean

In the order they appear in the frontmatter:

- `landing`: `diff_only` stops at a patch, `commit` commits each verified task, `pull_request` pushes a branch and opens one.
- `on_ambiguity`: `halt` stops and asks; `assume_and_document` picks the conservative option and records it on the task.
- `milestone_approvals`: pause for a human after every wave of work.
- `work_branch`: the branch to commit on; it defaults to `gm/<id>`.
- `secrets`: names only, never values. The harness asks for each one and keeps it in its vault.
- `env`: ordinary settings the program reads, with an optional default. A name cannot be both a secret and a setting.
- `network`: the hosts the build may reach; `critical: true` fails the task when a request fails, and `*.example.com` matches a whole domain.
- `budget`: the largest prompt for one task, and how many rewrite rounds a failed task gets.

## Why they are written the way they are

Each brief follows the practices in section 5 of the spec, which come from briefs that failed to build:

- The program's `main` is in `cmd/<name>/`, and acceptance commands run the built binary by name, never `go run`.
- Input and output go through `io.Reader` and `io.Writer`; tests are table driven and never touch the network or real files.
- Every boundary a test will assert has an exact answer: empty input, out of range, malformed, over the limit.
- Output text, flag names and exit codes are written out in full.

## Test fixtures, not examples

These briefs sit next to the code that tests them and are not shipped:
`gophermind-lib/build/planner/testdata/` (`greeter`, `csvstat-cli`),
`gophermind-lib/build/executor/testdata/greeter/`, and `gophermind-lib/build/testdata/`
(`example`, and the 750-line AI Venture Studio brief).
