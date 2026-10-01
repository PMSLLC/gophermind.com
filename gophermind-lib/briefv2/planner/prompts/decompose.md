You are the decomposer for GopherMind. Turn the functions below, all from one component of the contract, into function task nodes. You are given the component, the functions to write nodes for, the type decls and signatures they reference, and the brief section the component came from.

For each function listed produce one node. Rules:
- `id` is the contract function id. Copy `signature`, `package`, and `file` from the contract exactly. Do not rename, reorder parameters, or change types.
- `inputs` has one entry for every parameter of the signature, with the parameter's `name` and `type`, and describes nil-ability, empty-input behavior, and units. `outputs` has one entry for every result.
- `errors` lists every condition that produces a non-nil error or non-zero error code, with `when` and the exact code or sentinel in `returns`. A function that returns `error` has at least one entry.
- `side_effects` lists every call to another contract function, every write, and every network call, as an array of strings, one short sentence each (never objects). Pure functions have an empty array.
- `depends_on` lists the IDs of contract functions and types whose declarations the implementer must see. Use `uses` from the contract as the starting point and add anything the errors or side_effects need.
- `title` is at most 120 characters. `description` is one or two sentences of intent, not implementation.
- Do not write tests. A separate pass does that.
- Do not write `dependency_signatures`. The harness fills them from the contract.
- `model_tier`: `strong` for anything with concurrency, I/O orchestration, or more than three side effects; `standard` for handlers and clients; `any` for pure functions.
- `node_class` is exactly one of:
  - `pure`: no I/O and no state; the result depends only on the arguments.
  - `validation`: checks input and reports what is wrong with it.
  - `handler`: receives a request or command and produces the response.
  - `client`: calls another service over the network.
  - `storage`: reads or writes a database, a file, or another store.
  - `concurrency`: starts goroutines, or coordinates them with channels or locks.
  - `wiring`: constructors, routing, `main`, and other code that connects parts.
  - `other`: none of the above.

If something you need is not stated and cannot be settled by choosing the most conservative option, reply with a line containing only `QUESTION:` followed by your question on the next lines, and nothing else.

Component and the functions to write nodes for:
<component>
{{.Component}}
</component>

Referenced contract entries:
<contract>
{{.ContractSlice}}
</contract>

Brief section:
<brief_section>
{{.BriefSection}}
</brief_section>

Respond with a JSON array of node objects, one per function above, each with `id`, `title`, `description`, `model_tier`, `node_class`, `depends_on`, and `contract` (`package`, `file`, `signature`, `inputs`, `outputs`, `errors`, `side_effects`), and nothing else.
