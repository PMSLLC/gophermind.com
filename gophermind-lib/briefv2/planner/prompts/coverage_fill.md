You are the coverage auditor for GopherMind. The plan below leaves some requirements of the brief uncovered. Close each gap.

Uncovered requirements, each with the reason it counts as uncovered:
<gaps>
{{.Gaps}}
</gaps>

Plan nodes (id | kind | parent | title | file):
<nodes>
{{.Nodes}}
</nodes>

Components (id, package):
<components>
{{.Components}}
</components>

For each gap do one of these:
- Map it to function or component nodes from the list that already satisfy it and were overlooked.
- Add a root test: a shell command, run from the repository root, that exits 0 only when the requirement holds. An acceptance requirement (ids starting with A) always needs one.
- Declare the functions, and any types, that are missing from the plan. Give each function the `component` it belongs to (an id from the list), and map the requirement to the new function ids.

Use only requirement ids from the gap list. Do not repeat a function that is already in the node list. There is no limit on how many functions you may declare.

Respond with one JSON object and nothing else:
{"map": [{"requirement": "C1", "nodes": ["fn-..."]}], "root_tests": [{"requirement": "A1", "name": "...", "given": "...", "expect": "...", "command": "..."}], "types": [<type>], "functions": [<function>]}

<type> and <function> match these schemas:
<schema>
{{.ItemSchemas}}
</schema>
