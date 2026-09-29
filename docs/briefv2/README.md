# GopherMind v2: brief to build (offline foundations)

v2 turns a written brief (a markdown file with YAML front matter) into a tree of
task nodes that agents build, wave by wave, against a shared contract. This
directory documents the offline foundations only: brief loading, the secret
vault, the node tree with waves, contract slicing, and the run directory. No
model calls happen in any of it.

## Commands

```text
gophermind brief validate <brief.md>
gophermind brief vault set <NAME>      (value from a terminal prompt or stdin)
gophermind brief vault list
gophermind brief tree check <run-dir>
```

Anything else under `gophermind brief` prints this usage and exits 1. The run,
resume, status and report subcommands arrive in later plans (deviation D1).

Exit codes: 0 ok, 1 error (usage, unreadable file, failed check), 2 invalid
brief (the message names the offending field). Codes 3 and 4 are reserved for
human gates in a later plan.

`validate` prints one `warning:` line to stderr for each undeclared token that
looks like a secret name. Warnings never change the exit code.

`tree check` loads the run directory, then requires a well-formed tree: exactly
one root, every component's parent is that root, and every function's parent is
a component in the tree. An empty or wrong directory fails with exit 1. Two
nodes that would be written to the same file are rejected when the tree is
built. Readiness of a component or the root looks at its children only and
ignores its own `depends_on` (D2).

## Vault

Secrets are stored age-encrypted, in scopes: `harness` (used by
`brief vault set` and `list`) and `run/<brief-id>` (a brief's declared secrets).

- `GOPHERMIND_VAULT_PATH` overrides the vault file. The default is
  `<config dir>/vault.age`.
- `GOPHERMIND_VAULT_PASSPHRASE` supplies the passphrase without a prompt. It is
  required when stdin is a pipe, because the value for `vault set` is then read
  from that pipe and the passphrase must not compete for it.
- A piped value is stored whole: all of stdin is the value, including embedded
  newlines (so `cat key.pem | gophermind brief vault set KEY` keeps the full
  PEM), minus exactly one trailing newline. Empty input is an error.
- The vault file is fsynced before the rename and its directory is synced after.

## Run directory

`.gophermind/<brief-id>/` is created with mode 0700 (`brief.md` is 0600) and the
entry `.gophermind/` is added to the repository's `info/exclude`. In a git
worktree, where `.git` is a file, the exclude entry goes into the main
repository's `info/exclude` (found through `gitdir:` and `commondir`). A `.git`
that cannot be resolved is an error and nothing is created; a directory with no
`.git` is left alone.

## Environment block

The frontmatter `env` section declares non-secret variables that a command may
read. Each declares a name, purpose, and optional default value. An env
name must not equal a secret name; overlap is rejected with exit 2 and the
message `ENV_SECRET_OVERLAP: <name>`.

`brief validate` also rejects, with exit 2, a secret name declared twice
(`secret <NAME> is declared twice`), an env name declared twice (`env <NAME> is
declared twice`), and any secret or env name reserved for the harness
(`<NAME> is reserved for the harness`). The ten reserved names are
HTTP_PROXY, HTTPS_PROXY, NO_PROXY, GOPHERMIND_NODE (proxy and node variables
the harness emits) and PATH, HOME, GOCACHE, GOMODCACHE, GOPATH, TMPDIR
(toolchain variables, see E5). The set lives in `brief.IsReservedName`, which
`execenv.Build` also uses.

## Secret-name scan rule

The `brief validate` command scans the brief body for undeclared tokens that
look like secret names. Only UPPER_SNAKE_CASE tokens (three or more characters)
are considered. A token is flagged when it (a) ends in `_KEY`,
`_SECRET`, `_TOKEN`, `_URL`, `_DSN`, `_PASSWORD`, or `_PASSPHRASE`, or (b)
sits on a line containing "secret", "credential" or "environment variable"
(case-insensitive). Undeclared tokens matching the secret suffix list or
appearing on a line with secret wording are printed as warnings to stderr; they
never change the exit code. Declared secret and env names are always exempt.

## Command environment

The `execenv.Build` function constructs the exact environment handed to an
exec.Cmd for running a node's commands. It includes the declared env variables
(applying defaults, then config overrides from the harness), the declared
secrets (fetched from the vault), HTTP_PROXY and HTTPS_PROXY (when a proxy URL
is given), and GOPHERMIND_NODE (the node id). Nothing from the harness process
environment is included. A secret name never has a default or an override. The
ten reserved names listed above are rejected if a brief declares them as env or
secret names, and `Build` itself also checks that every name matches
`^[A-Z][A-Z0-9_]*$`.

The optional `Inputs.Toolchain` map holds PATH, HOME, GOCACHE, GOMODCACHE,
GOPATH and TMPDIR. It is harness-owned, filled from harness config, and `Build`
never reads the process environment for it. Any other key or an empty value is
an error. When it is nil or empty the environment is exactly the strict list
above. It exists because that strict list cannot run `go test` (no build cache
without GOCACHE and HOME), so it deliberately widens the handoff's literal
list (E5). The result is sorted NAME=value strings suitable for exec.Cmd.Env.

## Decisions

E1 comes from the handoff; E2 to E5 are this project's decisions:

E1: The secret-name scan rule has no stoplist. A token is flagged based on
suffix or line wording, not a whitelist.

E2: The harness config format is still undecided, so overrides are passed to
the builder as a map; wiring them to config is a later task.

E3: The test fixture uses the updated brief version that includes the env
block, yielding 2 scan warnings (ceiling 3) on the venture-studio brief.

E4: The proxy URL is optional; empty means HTTP_PROXY and HTTPS_PROXY are
omitted. GOPHERMIND_NODE is always set. Once the network proxy exists the
executor must always set it.

E5: The handoff says nothing else may reach commands, but the exact environment
cannot run `go test`. `Inputs.Toolchain` is an optional harness-owned map of
six variables; nil means strict.

## Package map

```text
gophermind-lib/briefv2/
  schema/     embedded JSON schemas, Validate(kind, json)
  testdata/   example brief, contracts.json and a partial node tree
  brief/      front matter, sections, secret-name scan
  vault/      age-encrypted secret store, prompts
  tree/       nodes, cycle check, waves, readiness, file store
  contract/   load, Slice, Diff, Affected
  rundir/     .gophermind/<id>/ layout, kept out of git via .git/info/exclude
cmd/gophermind/brief.go   the `gophermind brief ...` command group
docs/briefv2/handoff/     the original handoff, for reference (copied from the zip; the only edits are that
                           interfaces/blackboard.go was gofmt'd and interfaces/ has a go.mod so the repo's
                           Go tooling skips it: it holds two packages, blackboard and provider, meant to be
                           copied into real packages later)
```

## Deviations from the handoff

The handoff's stated tests cannot all pass against its own example data. These
resolutions reproduce every value in the examples that can be checked.

| # | Handoff says | Problem | Resolution in this plan |
|---|---|---|---|
| D1 | CLI is `gophermind run`, `resume`, `status`, `report`, `validate`, `vault`, `tree` | `run`, `resume`, `status`, `report` already exist as commands | Namespace everything as `gophermind brief <sub>` (`brief validate`, `brief vault set`, `brief tree check`, later `brief run`, ...) |
| D2 | Wave rule: components take the max wave of their children; function with empty `depends_on` has no defined wave | The example has `registration` at wave 1 with a child at wave 2, and `fn-validation-error-error` (no deps) at wave 0 | `wave = 0` if `depends_on` is empty, else `1 + max(wave of depends_on)`, for every kind. Children do not affect a node's wave. Readiness (children verified) is separate |
| D3 | Slicing `fn-register-handler` yields "the seven entries shown in its node file" | The stated algorithm over the shipped `contracts.json` yields 10 entries (4 types, 6 functions). The node file's 7 entries are hand-written, include a `CRM` interface and a `Server` struct that `contracts.json` does not define, and omit 5 signatures | The golden test asserts the algorithm's 10 entries. The shipped `contracts.json` is used unchanged. Open item for John: the contract is missing `Server` and `CRM` types that `fn-server-new` relies on |
| D4 | "Every file under examples/tree validates", waves reproduced | The example tree is a partial excerpt: only 7 node files, while `depends_on` and `children` reference ~10 more | Per-file schema validation runs on all 7. Tree-level tests use the 6-node subset whose dependencies exist, plus synthetic trees. `Load` does not require `children` to resolve |
| D5 | Secret scan regex `\b[A-Z][A-Z0-9_]{2,}\b` minus a stoplist | It flags 5 error codes in the example brief (`EMAIL_INVALID` etc.) | The handoff update resolved the scan noise. Venture-studio fixture: 2 warnings, ceiling 3. Warnings only, never blocking |
| D6 | Config in `gophermind.yaml`, then TOML | `GOPHERMIND.toml` is not parsed by any Go code and there is no TOML library | Items 1 to 4 need no harness config. Vault path defaults to `<config.Dir()>/vault.age`. Config format is decided in the provider/router plan |
| D7 | Blackboard: reuse the recursive agent system's | Not in this repo; John did not say where it lives | Assumption: build the SQLite backend in the blackboard plan (item 6). Not needed here |
| D8 | Live view (item 15) is a wave board plus attempt log | John wants a Gantt chart | Item 15 becomes a Gantt view: one row per leaf grouped by component, bars from each attempt's `started_at` and `duration_ms`, one bar per model in the fallback chain, dependency arrows from `depends_on`, rate-limit waits as gaps, revision rounds as a new block. It reads the blackboard only and works live and after the fact (no forecast before a run, since durations are unknown). No change to items 1 to 4 or to the item 6 blackboard interface. |
