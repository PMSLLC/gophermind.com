# slugify fixture set

Origin: the recorded planner replies of run `slugify-v1-20261105-r001`, made on
2026-11-05 with build 0932dc1 and the model gemma-4-26b-a4b on the mini. They
were extracted with `scripts/extract-llm-fixtures.py` (every call except the
`implement:*` ones, 35 files). `brief.md` is the run's brief with the `repo:`
value replaced by `REPO_DIR`, which the test rig fills in.

Defects these replies show (a regression test can assert the planner now
handles them):

- The feature text is missing from the node packets.
- Packages come back as `slug`, `cli` and `main` instead of directories.
- A second entry component, `entry-point-main`, appears next to `entry-point`.

Rule for later edits: a recorded file is never changed. A corrected reply a
later task needs goes in the next `.N.txt` slot, or under the stage name the
fixed planner now asks for, and is listed below with its reason.

Corrected replies:

- `contract.cli-logic.2.txt`: the reply to the fixed-interface repair pass.
  The recorded `contract.cli-logic.txt` declares `run` (and `main`) in
  package `main`, which the planner now drops as foreign to cli-logic, so
  cli-logic lacks the brief's fixed `Run` and is asked once for it. This
  reply declares `Run` in `internal/cli` with the brief's signature. Its id
  is `fn-run` (it was `fn-cli-run` until Task 6), the id the recorded
  decompose, enrich and testwrite replies use for `Run`; with it the
  entry-point reply's own `fn-run` (lowercase `run` in package main) is a
  repeat of an id already written and is dropped. Since coverage checks the
  behaviour (Task 7), its doc also states the Report errors messages and
  exit codes, `usage: slugify [--max N] [FILE]` and `slugify: invalid --max
  "<value>"` with 2 and `slugify: <error>` with 1, as a careful contract does.
- `coverage_fill.txt`: the fill reply for the Task 7 gap on F2: the recorded
  map gives F2 to fn-parse-args and fn-print-error, whose text carries none
  of its messages, and this reply maps F2 to fn-run as well.
- `decompose.entry-point.txt`: a copy of `decompose.entry-point-main.txt`.
  The fixed planner has one entry component, `entry-point`, and asks
  `decompose:entry-point`; the recorded reply was for `entry-point-main`.
  Its content needed no id change (it holds `fn-main` only).
- `enrich.entry-point.txt`: a copy of `enrich.entry-point-main.txt`, for the
  same reason (`enrich:entry-point` is asked; the reply holds `fn-main`).
- `enrich._fix.3.txt`: the third enrich repair call. `fn-run` is now declared
  last in cli-logic, so the enrich batches are `fn-parse-args`,
  `fn-execute-slugify`, `fn-read-inputs`, `fn-print-error` and then `fn-run`,
  `fn-cfg-options`; the recorded batch replies answer the r001 batches, so
  `fn-print-error` and `fn-run` come back `group_missing`. The recorded
  `enrich._fix.txt` answers the first cli-logic repair (performance of
  `fn-print-error` only) and `enrich._fix.2.txt` the `fn-main` repair. This
  reply holds the recorded groups of `fn-print-error` (from
  `enrich.cli-logic.2.txt`, without its `performance`, which the repair did
  not ask for and which was not applicable there) and of `fn-run` (from
  `enrich.cli-logic.txt`), unchanged. Since the enrichment behaviour fix
  (Task 6) the outline conventions' "No logging allowed" makes a log event a
  defect, so `fn-read-inputs` is repaired too: this reply also holds its
  recorded groups from `enrich.cli-logic.txt`, and the log events of
  `fn-run` and `fn-read-inputs` are empty lists.

Files:

- `brief.md`
- `clarify.txt`
- `contract._repair.txt`
- `contract.cli-logic.2.txt`
- `contract.cli-logic.txt`
- `contract.entry-point.txt`
- `contract.outline.1.txt`
- `contract.outline.2.txt`
- `contract.outline.entry.2.txt`
- `contract.outline.entry.txt`
- `contract.slug-logic.txt`
- `coverage.txt`
- `coverage_fill.txt`
- `decompose.cli-logic.txt`
- `decompose.entry-point-main.txt`
- `decompose.entry-point.txt`
- `decompose.slug-logic.txt`
- `enrich_comp.cli-logic.txt`
- `enrich_comp.entry-point-main.txt`
- `enrich_comp.entry-point.txt`
- `enrich_comp.slug-logic.txt`
- `enrich_root.txt`
- `enrich._fix.2.txt`
- `enrich._fix.3.txt`
- `enrich._fix.txt`
- `enrich.cli-logic.2.txt`
- `enrich.cli-logic.txt`
- `enrich.entry-point-main.txt`
- `enrich.entry-point.txt`
- `enrich.slug-logic.txt`
- `testwrite.fn-cfg-options.txt`
- `testwrite.fn-execute-slugify.txt`
- `testwrite.fn-make.2.txt`
- `testwrite.fn-make.3.txt`
- `testwrite.fn-make.4.txt`
- `testwrite.fn-make.fix.txt`
- `testwrite.fn-make.txt`
- `testwrite.fn-parse-args.txt`
- `testwrite.fn-print-error.txt`
- `testwrite.fn-read-inputs.txt`
- `testwrite.fn-run.txt`
