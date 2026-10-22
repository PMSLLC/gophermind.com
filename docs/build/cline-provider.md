# The Cline provider

A provider of `kind: cline` lets the Cline CLI (https://github.com/cline/cline) build a leaf. The router asks it for one reply like any other provider. Instead of generating text, it runs Cline headlessly in a scratch copy of the leaf's worktree, lets Cline edit and test there, and returns the finished target file as one fenced Go reply.

## Why a provider and not an executor

The executor owns the stub and real file swap, the gates, the commit and the failure notes. A provider keeps all of that. The reply goes through `packer.ParseReply`, the import policy, the source scan and the leaf's tests exactly as a reply from a chat model does, so Cline can only ever contribute the text of one file. A failed attempt gets the normal failure class and the normal fixed failure note in the next prompt. Routing, ladders, escalation and the ledger work unchanged.

## Data flow

1. The executor builds the leaf prompt and a `provider.Request` with `Workdir` (the leaf worktree, absolute) and `TargetFile` (slash-separated, repo relative).
2. The provider validates both, then copies the worktree into a fresh temp directory. The copy leaves out every `.git` entry and every symlink and is capped by size (64 MiB) and file count (5000).
3. It writes an isolated Cline data dir with settings that force act mode, telemetry off and auto-update off, and symlinks your `providers.json` into it. Your real Cline data dir is never written. Cline's default Plan mode refuses to write files, hence the isolated settings.
4. It runs `cline --json --auto-approve true --data-dir <iso> -P <provider> -m <model> -c <scratch> -t <secs> <prompt>`. The prompt is the request messages plus a trailer that names the target file and asks Cline to write only that file and run `go test`.
5. The child gets an environment allowlist (PATH, HOME, TMPDIR, LANG, LC_ALL and the Go variables). No API key or token from the parent process reaches it. It runs in its own process group, which is killed on timeout or cancel.
6. When Cline exits, the provider reads back only the target file from the scratch copy: it must be a regular file, not a symlink, inside the copy, at most 1 MiB, with no line that is a bare code fence. Everything else Cline wrote is discarded with the scratch directory.
7. The text is returned as a fenced Go block. Token usage comes from Cline's `run_result` event (`inputTokens` as prompt tokens, `outputTokens` as completion tokens).

A `done` event or a zero exit is not success. The file and the gates decide. Auth, rate-limit and balance errors map to the router's typed errors; the prompt is never put in an error message.

## Configuration

```yaml
providers:
  - name: cline
    kind: cline
    visibility: public
    cline_provider: cline-pass          # the -P value
    cline_binary: cline                 # default
    cline_settings_dir: ~/.cline/data/settings   # default; a leading ~/ is expanded
    cline_timeout_seconds: 900          # default
    models:
      - id: cline-pass/qwen3.7-max
        context_tokens: 131072

models:
  standard: [cline/cline-pass/qwen3.7-max]
```

`visibility: public` is required in practice: Cline sends the code to Cline's service, so the privacy mode must allow a public provider. Preflight and the proxy allowlist skip `kind: cline` providers.

## Setup

1. `npm i -g cline`
2. `cline auth` and sign in.
3. Pick the account type. ClinePass uses provider id `cline-pass` and model slugs of the form `cline-pass/<model>`. Cline credits use provider id `cline`. Use the matching `cline_provider` value and model id.
4. Check that `~/.cline/data/settings/providers.json` exists. Without it the provider reports an authentication error.

## Live test

The live test spends a real subscription and is skipped unless asked for:

```
cd gophermind-lib
GOPHERMIND_CLINE_LIVE=1 go test -count=1 -v ./build/provider -run TestClineLive
```

Optional variables: `GOPHERMIND_CLINE_BIN` (default `cline`), `GOPHERMIND_CLINE_PROVIDER` (default `cline-pass`), `GOPHERMIND_CLINE_MODEL` (default `cline-pass/qwen3.7-max`). It builds a small throwaway module, asks Cline for `Summarize`, writes the returned source into the module and requires `go test` and `go vet` to pass.

## Known limits and risks

- Cline makes its own network calls, outside the harness proxy and its allowlist.
- Cline sends repository code to Cline's service. Only the scratch copy is exposed, but that copy is the whole worktree (minus `.git` and symlinks). The copy is currently a plain copy of the worktree directory, so anything untracked in it, including the `.gophermind` run folder when the run lives inside the repository, is part of what Cline can read.
- Auto-approve lets Cline run shell commands as the current user inside the scratch directory. There is no OS sandbox around it; the environment allowlist and the scratch copy limit what it finds, not what it can do.
- The leaf's stub file (`zz_gm_stub_<id>.go`) is present in the worktree during the call, so the scratch copy contains it. If Cline declares the same function in the target file and runs `go test`, the scratch package may fail to compile with a redeclaration even though the real swap would pass.
- The router's budget and ledger see only the token usage Cline reports.
- Graded runs that use only the local model must not include a cline provider.
- Windows is not supported; the provider is a stub there and always fails.
