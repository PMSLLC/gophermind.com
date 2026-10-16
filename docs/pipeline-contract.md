# Pipeline run view contract (schema_version 1)

The server (`gophermind-server`, routes in `gophermind-lib/serve`) serves a read-only view of the newest or a named run folder (`<root>/.gophermind/<run-id>/`). The desktop app (`gophermind-osx/client`) and the web dashboard (`GET /pipeline`) decode the same bodies. Everything is read through `gophermind-lib/runview`, which never writes to the run folder, keeps no cache and has no watcher: a stream and a fresh state call read the same files.

The wire types are in `gophermind-lib/runview/types.go`; the contract goldens in `gophermind-lib/runview/testdata/golden/` are decoded by the client tests.

## Versions

- Every JSON body that runview builds carries `"schema_version": 1` (`runview.SchemaVersion`). Every response carries `X-Gophermind-Pipeline-Schema: 1`, including 401, 4xx and 5xx answers. A client that sees another number reports a server and app disagreement and does not decode the body.
- The final report is the exception: `GET /pipeline/report` returns the executor's `report.json`, whose `schema_version` is `report.SchemaVersion` (3 today). It is a different number from the pipeline contract version and the two move independently.

## Auth and common rules

- All routes except the dashboard page `GET /pipeline` need `Authorization: Bearer <token>`. The page itself carries no data.
- All routes are GET. Every route accepts `?run=<run-id>`. Without it the run is the most recently updated one (newest file modification time in the run folder, run id descending as the tiebreak).
- Ids are checked against patterns. A bad run id, node id, attempt number or artifact name gives 400. A missing run, node or file, or one reached through a symbolic link, gives 404.
- Errors are JSON `{"schema_version": 1, "error": "<fixed message>"}`: 400 `invalid id or parameter`, 404 `not found`, 500 `internal error`. A message never echoes the request or a path.

## Routes

### GET /pipeline/runs

`RunsBody`: `runs`, newest first (most recently updated first). Each entry is a `RunSummary`: `run_id`, `title`, `repo_name` (the folder name, never a path), `status`, `stop_reason`, `planned_at`, `started_at`, `updated_at`, `waves`, `counts` (`nodes`, `by_status`; function nodes only) and `coverage` (`covered`, `total` requirements). A repo with no run gives `"runs": []`.

### GET /pipeline/state

`StateBody`: `generated_at`, `event_seq`, `run` (a `RunSummary`), `nodes` (`NodeSummary` list) and optional `warnings` (ids and line numbers only, never content). With no `?run=` and no run in the repo the answer is 200 with `run: null` and `nodes: []`; with an unknown `?run=` it is 404.

`event_seq` is the line count of `_state/events.jsonl`, read before the nodes, so a client resumes the stream at exactly this snapshot with `since=<event_seq>`.

Run status is derived from the files present:

1. `report.json` exists: its `status` and `stop_reason` (a failed report with `harness_fault` is shown as status `harness_fault`; an unreadable report is `failed` with reason `report_unreadable`).
2. No report and no `_state/executor.json`: `status` and `stop_reason` from `_state/project.json` when it has them (a `/project` run that stopped before the executor).
3. `_state/executor.json` exists: `building`.
4. `approval.json` exists: `planned`.
5. `_state/status.json` says it is waiting: `waiting`.
6. Otherwise `planning`.

Statuses: `planning`, `waiting`, `planned`, `building`, `verified`, `failed`, `escalated`, `interrupted`, `harness_fault`.

A `NodeSummary` has `id`, `kind`, `parent`, `title`, `status`, `wave`, `model_tier`, `node_class`, `depends_on`, `requirement_ids`, `revision`, `claimed_by`, `attempt_count`, `last_attempt`, `result` and `tests`. Component and root nodes have no runtime file: their status is derived (verified when every child is verified, otherwise the worst child) and the status stored in the plan file is ignored for them.

### GET /pipeline/node?id=<node-id>

`NodeBody`: `node` (the stored plan node, all groups and its embedded decisions, as raw JSON), `runtime` (the runtime file: status, revision, claim, heartbeat, attempts, result; `null` before the executor started) and `artifacts` (per attempt, the saved file names).

### GET /pipeline/artifact?id=<node-id>&attempt=<n>&file=<name>

The text of one saved attempt file (for example `check-output.txt`), as `text/plain; charset=utf-8` with `X-Content-Type-Options: nosniff`. `attempt` is an integer of at least 1. The read is size-bounded.

### GET /pipeline/decisions

`DecisionsBody`: `decisions`, a list of raw records. The root node's records (the planning conversation) come first, then the build's revision decisions from `_state/revisions/decisions.json`; each source keeps its stored (append) order, not a sorted order. A damaged revision file adds a warning to state and is skipped.

### GET /pipeline/report

A finished run returns `report.json` byte for byte, after it passes the same checks `report.Read` makes (known fields, `schema_version` from 1 to `report.SchemaVersion`, exit code consistent with the status). Its `schema_version` is 3 today. The keys `revised_leaves`, `rewritten_leaves` and `parallel` are passed through verbatim when the report has them and are absent otherwise (`revised_leaves` and `rewritten_leaves` arrived with version 3; `parallel` was added without a bump).

A run that has no `report.json` yet returns a partial report built from the call log and the runtime files, with `"complete": false`. The partial report does not carry the revision and parallel totals; only the final report does. `IsComplete` treats an absent `complete` key as complete.

### GET /pipeline/events

A Server-Sent Events stream. Frames look like:

```text
id: 17
event: node-status
data: {"id":"fn-bye","status":"verified","wave":1,"revision":0}

```

Event types and their data:

| Event | Data | Source |
|---|---|---|
| `node-status` | `{id, status, wave, revision}`: the node's values as they are now. A status line and a revision line of the log both map to it, so `revision` is always present. | log line |
| `node-attempt` | `{id, attempt}` (an `Attempt`) | log line |
| `node-result` | `{id, result}` (`commit`, `files_changed`) | log line |
| `wave-changed` | `{wave, state}` with `state` `started` or `finished` | derived |
| `run-status` | `{status, stop_reason}` | derived |
| `run-report` | the final report body | derived |

The `id` of a node-* frame is the 1-based line number of `_state/events.jsonl`. `wave-changed`, `run-status` and `run-report` are derived from state as it changes and are not in the log, so they are **never replayed**. They carry the line number of the latest log line read when they fire, and no `id:` line when that is 0.

Where the stream starts:

1. `?since=N` wins.
2. Otherwise the `Last-Event-ID` request header.
3. With neither, the stream starts at the current end of the log (live only).
4. A value that is not a non-negative integer is 400.
5. A value larger than the log length replays from 0 (an id from another run, for a client that reconnects after a newer run became the default).

Lines up to `since` are read only to keep attempt counters right; lines after it are sent. Corrupt lines are skipped and reported as warnings, not sent.

Behavior of the connection:

- Headers and an empty first write go out at once, so a client is connected before the first event.
- A `: keepalive` comment is sent every 15 seconds. Every write has a deadline of 30 seconds (twice the keepalive); a client that stops reading is dropped and frees its slot. The log is polled every 250 ms.
- At most 16 streams run at once. Override with `GOPHERMIND_PIPELINE_MAX_STREAMS` (unset, invalid or below 1 means 16). One more gets 503 with `Retry-After: 5` and the JSON error body `too many event streams`. A client that disconnects frees its slot.

### Client rule for reconnects

Because derived events are never replayed, a client must refetch `GET /pipeline/state` and `GET /pipeline/report` on every connect and reconnect, then resume the stream with `since=<event_seq>` from the state it fetched. The desktop follower does this in its `OnOpen` hook, and restarts itself when the refetch shows a different run id.

## Files this view reads

`plan` nodes, node `*.runtime.json`, `_state/events.jsonl`, `_state/calls.jsonl`, `_state/executor.json`, `_state/project.json`, `_state/status.json`, `_state/revisions/decisions.json`, `approval.json`, `report.json` and `attempts/<node>/<n>/`. Nothing is created, renamed or touched.
