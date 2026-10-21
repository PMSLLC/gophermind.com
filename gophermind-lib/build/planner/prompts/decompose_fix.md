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
- `undeclared_field`: your description or contract text refers to a field or method (like `s.Sum`) that the declared struct of that parameter does not have; its `where:` line names the parameter, the type, the unknown field or method and the type's declared fields. The types are fixed by the contract and you cannot add a field. Rewrite the text so the behavior is stated using only the declared fields (and the function's other parameters and results), and do not mention the unknown field.
- `schema`: the node does not match the node schema; its `where:` line names each failing `field:` (`[]` is an array item) and the schema `keyword:` it breaks (`type`, `enum`, `required`, `additionalProperties`, ...).
- Shapes: `side_effects` is an array of strings, one short sentence each (never objects); `inputs`, `outputs` and `errors` are arrays of objects; `model_tier` is `strong`, `standard` or `any`, never a node class.

The nodes:
<nodes>
{{.Nodes}}
</nodes>

Respond with a JSON array with one object per node above: `id`, and only the fields you were asked to write (`contract` carries only the contract fields asked for), and nothing else.
