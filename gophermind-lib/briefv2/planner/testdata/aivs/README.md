# AI Venture Studio: the plan that motivated the Coverage stage

On 2026-09-29 the v1 planner (`plantree`) turned the AI Venture Studio server
brief into a plan of 21 phases, 47 tasks, and 99 steps, and the plan was
approved. Read against the brief afterwards, it dropped most of the brief's
constraints and every acceptance bullet. These two files keep that plan so the
coverage checker is tested against the failure it exists to catch. The brief
itself is `gophermind-lib/briefv2/testdata/ai-venture-studio-server-brief.md`.

## nodes.json

The v1 plan as plan nodes (`planner.PlanNode`): one root, each phase as a
component (`phase-001` to `phase-021`), each step as a function
(`p001-t001-s001` style) whose `file` is the step's first target path. Titles
only. It was generated once, from the repository root, from the v1 plan in
`.planning/plan/` (not tracked), with this script:

```python
import glob, json, os, re

def num(path):
    return re.search(r"(\d+)$", os.path.basename(path)).group(1)

nodes = [{"id": "gm-2026-09-29-002", "kind": "root", "title": "AI Venture Studio Server"}]
for ph in sorted(glob.glob(".planning/plan/phases/phase-*")):
    pm = json.load(open(ph + "/meta.json"))
    pid = "phase-" + num(ph)
    nodes.append({"id": pid, "kind": "component", "parent": "gm-2026-09-29-002", "title": pm["title"]})
    for tk in sorted(glob.glob(ph + "/tasks/task-*")):
        for st in sorted(glob.glob(tk + "/steps/step-*")):
            sm = json.load(open(st + "/meta.json"))
            n = {"id": "p%s-t%s-s%s" % (num(ph), num(tk), num(st)), "kind": "function", "parent": pid, "title": sm["title"]}
            paths = (sm.get("work") or {}).get("target_paths") or []
            if paths:
                n["file"] = paths[0]
            nodes.append(n)
out = "[\n" + ",\n".join(json.dumps(n, ensure_ascii=False) for n in nodes) + "\n]\n"
open("gophermind-lib/briefv2/planner/testdata/aivs/nodes.json", "w").write(out)
```

Do not regenerate it: the source plan is not in the repository.

## mapping.json

A `planner.CoverageReply` written by hand on 2026-09-29 after reading all 99
steps: for every constraint (`C1` to `C10`) and feature (`F1` to `F19`) of the
brief, the steps or phases that genuinely address it, or an empty list when
none does. `root_tests` is empty because the v1 plan had no acceptance tests.

Judgment calls, so a later reader can disagree on the record:

- `C1` (`gofmt` and `go vet` clean) is unmapped. One step (`p016-t001-s001`)
  lists "go vet passes on the new code" among its own criteria; no step checks
  the project.
- `C2` (allowed modules only) is unmapped. `p001-t001-s002` adds dependencies
  for HTTP routing, JWT, and UUID generation, which the constraint forbids.
- `C5` (argon2id parameters and token lifetimes) is unmapped. `p003-t001-s001`
  says "bcrypt/argon2" and no step states the lifetimes.
- `C7` (60 second LLM timeout, never inside a transaction) is unmapped.
  `p006-t002-s002` says only "ensure timeouts are set".
- `C10` (table-driven tests, integration tests skip without
  `TEST_DATABASE_URL`) is unmapped. Two steps mention the variable; none
  mentions skipping or test layout.
- `C4` (the `company_id` SQL scan) and `C9` (60-line handlers) appear in no
  step at all.
