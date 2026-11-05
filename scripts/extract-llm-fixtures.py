#!/usr/bin/env python3
"""Turn a run's recorded model calls into a planner fixture set.

Usage: python3 -I scripts/extract-llm-fixtures.py STATE_DIR OUT_DIR

For every STATE_DIR/llm/N.json whose stage does not start with "implement:",
write its response to OUT_DIR/<stage with ':' replaced by '.'>.txt. The second
and later calls for the same stage (in id order) get .2.txt, .3.txt and so on,
the naming rule of planner.FixtureProvider. Reads nothing outside STATE_DIR.
"""
import json
import os
import re
import sys


def main(argv):
    if len(argv) != 3:
        print("usage: extract-llm-fixtures.py STATE_DIR OUT_DIR", file=sys.stderr)
        return 2
    state_dir, out_dir = argv[1], argv[2]
    llm_dir = os.path.join(state_dir, "llm")
    names = [n for n in os.listdir(llm_dir) if re.fullmatch(r"\d+\.json", n)]
    names.sort(key=lambda n: int(n[:-5]))
    os.makedirs(out_dir, exist_ok=True)
    seen = {}
    for n in names:
        with open(os.path.join(llm_dir, n), encoding="utf-8") as f:
            rec = json.load(f)
        stage = rec["stage"]
        if stage.startswith("implement:"):
            continue
        base = stage.replace(":", ".")
        seen[stage] = seen.get(stage, 0) + 1
        k = seen[stage]
        out = base + ".txt" if k == 1 else "%s.%d.txt" % (base, k)
        with open(os.path.join(out_dir, out), "w", encoding="utf-8", newline="") as f:
            f.write(rec["response"])
        print("%s -> %s" % (n, out))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
