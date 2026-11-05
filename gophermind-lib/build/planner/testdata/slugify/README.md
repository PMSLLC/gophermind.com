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

Corrected replies: none yet.

Files:

- `brief.md`
- `clarify.txt`
- `contract._repair.txt`
- `contract.cli-logic.txt`
- `contract.entry-point.txt`
- `contract.outline.1.txt`
- `contract.outline.2.txt`
- `contract.outline.entry.2.txt`
- `contract.outline.entry.txt`
- `contract.slug-logic.txt`
- `coverage.txt`
- `decompose.cli-logic.txt`
- `decompose.entry-point-main.txt`
- `decompose.slug-logic.txt`
- `enrich_comp.cli-logic.txt`
- `enrich_comp.entry-point-main.txt`
- `enrich_comp.entry-point.txt`
- `enrich_comp.slug-logic.txt`
- `enrich_root.txt`
- `enrich._fix.2.txt`
- `enrich._fix.txt`
- `enrich.cli-logic.2.txt`
- `enrich.cli-logic.txt`
- `enrich.entry-point-main.txt`
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
