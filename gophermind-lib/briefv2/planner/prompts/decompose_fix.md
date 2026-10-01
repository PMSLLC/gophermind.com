You are the decomposer for GopherMind. {{.Count}} function nodes you wrote have defects. For each node below write ONLY the fields listed after "fields to write", and nothing else about it.

Defect words:
- `missing_node`: you sent no node for this function; write every field listed.
- `node_class`: must be exactly one of pure, validation, handler, client, storage, concurrency, wiring, other.
- `title`, `description`: must not be empty; the title is at most 120 characters.
- `contract_missing`: the node has no contract object.
- `inputs_missing`, `input_type`: `inputs` needs one entry, with `name` and `type`, for every parameter of the signature (an unnamed parameter still needs one entry).
- `outputs_count`, `output_type`: `outputs` needs one entry, with `name` and `type`, for every result of the signature.
- `errors_missing`, `errors_incomplete`: a function that returns `error` needs at least one `errors` entry, and every entry needs `when` and `returns`.
- `depends_unknown`: `depends_on` names an id that is not in the contract.
- `schema`: the node does not match the node schema.

The nodes:
<nodes>
{{.Nodes}}
</nodes>

Respond with a JSON array with one object per node above: `id`, and only the fields you were asked to write (`contract` carries only the contract fields asked for), and nothing else.
