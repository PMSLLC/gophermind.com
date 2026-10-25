# Gopher, Burrow and GopherMind: Shared Spec

**Date:** 2026-10-24. **Status:** draft for John's approval. **Readers:** the Gopher session, the Burrow session, the GopherMind session, and John.

This is the one document all three components work from. Each section names its owner. A component may change only its own sections; a change that touches another component's section goes to that component's session as a message and is recorded in section 11. Where this spec and a component's own docs disagree, this spec wins until it is amended.

Related: `PMSLLC/gopher` plan `2026-10-24-gopher-core-language.md` (revision 2), GopherMind `docs/ROADMAP.md`, Burrow's design (private repo `PMSLLC/burrow`).

---

## 1. What each component is

| Component | Repo | Owner | Job |
|---|---|---|---|
| **Gopher** | `PMSLLC/gopher` | Gopher session | The plan language the model writes, and the Go library that parses, checks and derives from it. |
| **Burrow** | `PMSLLC/burrow` (private) | Burrow session | A library of verified functions keyed by language-neutral contract. Returns a function only when it is proven against the requester's vectors. |
| **GopherMind** | `PMSLLC/gophermind.com` | GopherMind session | The harness: planner stages, executor, run state, web interface, archive of finished runs. |

Gopher is the model's output format for the planner. Burrow never reads Gopher text. GopherMind is the only component that talks to both.

## 2. How they connect

```
brief
  -> GopherMind planner
       import stage: expand `reuse` from the archive        (section 7)
       decompose: model writes Gopher, GBNF-constrained      (section 3)
       gopher.Check, repair up to 2 rounds                   (section 3)
       testwriter: adds `t` vectors, edge rules enforced     (section 5)
  -> GopherMind executor, per leaf in wave order
       eligible leaf? gopher.Extract -> adapter -> Burrow lookup   (sections 4, 6)
         hit  -> write bundle, same gates as a built leaf
         miss -> build, then store in Burrow if eligible
  -> acceptance
  -> archive: canonical plan, origins, code refs              (section 7)
```

### Interfaces between components

| From | To | What | Defined in |
|---|---|---|---|
| model | GopherMind | Gopher text (optional think block first) | 3 |
| GopherMind | Gopher lib | `Parse`, `Check`, `Waves`, `Uncovered`, `Write`, `Extract`, `Eligible` | 3 |
| GopherMind | Burrow | lookup request: contract, key, requester vectors | 6.1 |
| Burrow | GopherMind | hit (bundle) or miss (with lookup trail) | 6.1 |
| GopherMind | Burrow | store request: contract, code, vectors, provenance | 6.4 |
| GopherMind | archive | final canonical plan, per-leaf origin, code refs | 7.1 |

---

## 3. Gopher language (owner: Gopher)

### 3.1 Document shape

- An optional `<think>...</think>` block, then the header `gopher 1`, then one record per line: `tag id field=value ...`.
- Record order is enforced: `brief`, `feat`, `reuse`, `req`, `comp`, `leaf`/`glue` (mixed), `uses`, `t`.
- Prose only in the rest of a `req` line and in `note=` (a JSON string).
- JSON values only in data fields: `given=`, `expect=`, `answers=`, `overrides=`, `note=`. No contract is ever written as JSON.
- Phases and waves are never written; they are derived.
- `uses A B C` means A depends on B and C.

### 3.2 Types

`string, int64, float64, bool, bytes, list<T>, map<string,V>, optional<T>, record{name:T,...}`. No spaces inside a type. Depth limit 32, length cap 1 KiB. `timestamp`, `decimal`, `enum` are reserved.

**Field order in a record is meaningful.** In `in=` it is the parameter order. `Type.String()` keeps source order. `Type.Key()` sorts fields by name and is used only for matching.

### 3.3 Leaf contract (one syntax for every leaf)

| Field | Required | Meaning |
|---|---|---|
| `in=` | yes | `record{...}` of inputs, in parameter order |
| `out=` | yes | `record{...}` of outputs |
| `effects=` | yes | `none`, or a list from `KnownEffects` (provisional: `db, net, fs, clock, rand, env, proc`) |
| `verb=`, `object=` | when `effects=none` | the intent, single identifiers |
| `traits=` | no | identifier list, for example `deterministic,total` |
| `failures=` | no | identifier list of named failure conditions |
| `burrow=`, `pin=` | see 3.5 | an exact Burrow function version |
| `from=` | see 3.5 | provenance of an imported record |
| `job=` | see 10 | unchanged from revision 1; meaning to be confirmed |

A leaf is **eligible for Burrow** when `effects=none` and `verb` and `object` are set. `gopher.Eligible` is the only definition; GopherMind and Burrow do not reimplement it.

### 3.4 Test vectors

`t <leaf> given={...} expect={...}`.
- `given` is a JSON object with one key per `in` field.
- `expect` is either a JSON object with one key per `out` field, or `{"fail":"<name>"}` where `<name>` is listed in the leaf's `failures=`.

### 3.5 Records the model may not write

- **`burrow=`/`pin=` and `from=` appear only on records inserted by the harness** (the import stage, section 7). A record without `from=` that carries `burrow=` or `pin=` is a `Check` error, so a model can never invent a Burrow key.
- The decompose grammar does not include `burrow`, `pin`, `from` or `reuse`.

### 3.6 `reuse` record

`reuse r1 from=<project>@<runid> reqs=q3,q4` asks the harness to import a feature from a finished run (section 7). It is written by the harness from the brief, never by the model, and is kept in the plan as a record of what was imported.

### 3.7 Reserved

`chain=` is reserved (deferred by John, 2026-10-24). `Check` rejects it with `chain is reserved`. Intended shape if revived: the leaf keeps its id and vectors, steps are exact type pipelines by `Key()`, the connector is generated by code, and the leaf's own vectors judge the result.

### 3.8 Library surface

`Parse(string) (*Doc, []Diag)`, `Check(*Doc) []Diag`, `Waves(*Doc) ([][]string, error)`, `Uncovered(*Doc) []string`, `Write(*Doc) string`, `Extract(Record) (BurrowFields, error)`, `Eligible(BurrowFields) bool`, `FromJSON([]byte) (*Doc, []Diag)` (experiment only). Diags carry a 1-based line number and a short fixed message, never more than 80 characters of input.

```go
type BurrowFields struct {
    Verb, Object string
    In, Out      []Field // source order
    Effects, Traits, Failures []string
}
```

`lang` never knows the `burrow-contract/v1` schema. The adapter (section 4) does.

---

## 4. Contract mapping (owner: GopherMind, adapter; schema owner: Burrow)

The adapter lives in GopherMind and is the only code that knows both sides.

| `BurrowFields` | `burrow-contract/v1` | Note |
|---|---|---|
| `Verb` | verb | |
| `Object` | object | |
| `In` | inputs | order preserved; Burrow's key ignores names and order |
| `Out` | outputs | |
| `Effects` | effects | must be `none` for lookup and store |
| `Traits` | traits | |
| `Failures` | failures | names compared exactly in v1 |

**Burrow to confirm:** exact field names, the effects vocabulary, the spelling rules for traits and failures, and the neutral type spelling Burrow uses. Until confirmed, the adapter is not built; Gopher's `KnownEffects` stays provisional.

**Parameter order:** the key ignores order, so a hit may store parameters in a different order. The executor calls a reused function with the stored contract's order, mapping by name (existing ROADMAP rule).

---

## 5. The accuracy guarantee (owner: all three; this section changes only with John's word)

### 5.1 What 100% means

- **Precision is 100%.** Burrow never returns a function that disagrees with the requester's vectors, and never returns one when two passing candidates disagree with each other.
- **Recall is not guaranteed.** A miss is always safe: the leaf is built as today.
- The guarantee is relative to the vectors. If the vectors cannot tell two behaviors apart, the cross-check (6.2 step 5) is the backstop. A wrong expected value in a vector causes a miss, never a false accept.

### 5.2 Edge vectors are required for eligible leaves (enforced by Gopher `Check`, written by GopherMind's testwriter)

For each `in` field, every edge value of its type appears in at least one vector (values may share a vector):

| Type | Edge values |
|---|---|
| `int64` | `0`, `1`, `-1`, min, max |
| `float64` | `0`, a negative fraction, a positive fraction |
| `string` | `""`, one character, `" "`, a non-ASCII string |
| `bytes` | empty, a single zero byte |
| `bool` | `true`, `false` |
| `list<T>` | empty, one element, three elements |
| `map<string,V>` | empty, one key |
| `optional<T>` | absent, present |
| `record{...}` | each field takes its edges in turn; no cross product |

Every name in `failures=` has at least one vector expecting it. `Check` verifies all of this by parsing `given` and `expect`. The model writes expected values; it cannot skip edges.

### 5.3 Proof

Burrow keeps an **adversarial catalog** of near-twins (base62 vs base64url, ceil vs floor vs round, trim vs trim_left, stable vs unstable sort, and more as found). A test requests each twin and asserts it never receives the other. **False accepts must be exactly 0**; any false accept fails Burrow's build. Hit rate is tracked separately and has no target.

---

## 6. Burrow (owner: Burrow)

### 6.1 Lookup

**Request:** contract (`burrow-contract/v1`), key, requester vectors (from the leaf's `t` lines), and optionally an exact `burrow=` version with `pin=` (from an import).

**Response:** either
- **hit:** bundle id, bundle version, root contract, code files, the parameter order of the stored contract, and the lookup trail; or
- **miss:** the lookup trail and a reason (`no_candidate`, `all_failed`, `ambiguous`, `not_eligible`).

**Lookup trail** (stored by Burrow, shown by GopherMind): the key, the candidates considered and how each was found (exact key, embedding rank), each gate result, the cross-check result, and the verdict.

### 6.2 The gate (every candidate, including exact-key hits and pinned versions)

1. **Candidates.** Exact key first, then top 3 by embedding (after Burrow phase 4). The embedding nominates; it never decides.
2. **Type gate.** Signature matches by `Key()`; effects `none`; failure names equal.
3. **Vector gate.** Run the candidate in the sandbox against the **requester's** vectors. Every vector must pass. The candidate's own stored tests are not used here (they would be circular).
4. **No model in the accept path.** A model check may filter candidates before step 3, never accept one.
5. **Cross-check.** If more than one candidate passes step 3, run all passing candidates on 200 inputs generated from the types (seed derived from the key, recorded in the trail) and compare outputs. Any disagreement: return miss with reason `ambiguous`. If all agree, return the one with the smallest bundle.

### 6.3 Bundles

- A stored function may call other stored functions. The unit returned is a **bundle**: the root plus its helpers, each pinned by version.
- **Bundle version** is a hash over the root's code and every helper's pinned version. Updating a helper creates a new bundle version; a pinned version never changes.
- Only the root is verified against the requester's vectors. Helpers were verified when stored.
- Eligibility widens from "stdlib only" to "stdlib or pinned Burrow functions". The catalog rejects a store that would create a dependency cycle.
- Burrow does not search for or assemble compositions (section 3.7).

### 6.4 Store (on a miss)

Stored only when the built leaf is eligible, passed every gate, and its tests are expressible as vectors. The store request carries the contract, code, vectors, and provenance (project, run id, leaf id). Pure Go, no `os`, `net`, `os/exec`, `syscall` or `unsafe` imports (existing rule).

### 6.5 Miss log

Every miss is kept with its full contract, vectors and trail. This is the data for revisiting chains (section 3.7) and for tuning embeddings.

---

## 7. GopherMind harness (owner: GopherMind)

### 7.1 Archive

Every run that reaches acceptance writes, under the run directory:
- `plan.gopher`, the final canonical plan from `gopher.Write`;
- per-leaf origin, and for reused leaves the bundle id and version;
- code references (paths and commit) for every leaf.

An index maps project name to its runs, so a brief can name `<project>@<runid>` or `<project>@latest`. The location of the index on the server is decided by GopherMind (open, section 10).

### 7.2 Planner changes

- **Decompose emits Gopher**, constrained by the GBNF, thinking unconstrained, after the experiment gate in the Gopher plan passes. Until then the planner keeps its JSON.
- **Repair:** up to 2 rounds, feeding `Check` diags back. Repair prompts name the defect, never quote the reply.
- **Testwriter** writes `t` lines meeting section 5.2 for every eligible leaf.

### 7.3 Import stage (project feature reuse)

New stage, after clarify and before decompose. It runs when the brief names a feature from a finished run.

1. Read the archived `plan.gopher` of that run.
2. Take the named requirements, the nodes that `covers=` them, and the transitive `uses` closure.
3. Renumber ids into the new plan and merge requirements with the new brief's (duplicates confirmed in clarify).
4. Insert records by kind, each with `from=<project>@<runid>:<old id>`:

| Old node | Comes over as | Then |
|---|---|---|
| Pure leaf with a stored bundle | leaf with `burrow=` and `pin=` | goes through Burrow's gate (6.2) with its vectors |
| Pure leaf never stored | leaf with its contract and vectors | built normally |
| Effectful leaf | leaf with its contract and vectors, plus a code reference | built normally, old code given to the builder as a reference |
| Glue | not imported | the decomposer writes new glue |

5. Imported records go to the decomposer as fixed context. It may `uses` them but must not change them; `Check` compares each `from=` record with its archived source and reports any change.
6. The `reuse` record stays in the plan.

### 7.4 Origin values (unchanged)

`built`, `reused` (with bundle id and version), `lookup_missed`, `not_eligible`. Imported provenance is the record's `from=`, not a new origin value.

### 7.5 Run report and web interface

The report lists reused leaves and imported records. The web interface shows each leaf's origin, `from=` if present, and Burrow's lookup trail.

---

## 8. Metrics (owner: each, for its own events)

| Metric | Owner | Source |
|---|---|---|
| Tokens in, think, out, and repairs per planner stage | GopherMind | Gopher `metrics.Event` |
| First-try valid rate of Gopher replies | GopherMind | `Check` results |
| Lookups, hits, misses by reason | Burrow | lookup trail |
| False accepts on the adversarial catalog (must be 0) | Burrow | catalog test |
| Leaves built vs reused vs imported per run | GopherMind | run report |

---

## 9. Order of work

| Step | Component | Depends on |
|---|---|---|
| 1. Gopher core through the experiment gate (plan tasks 1 to 8) | Gopher | none |
| 2. Burrow confirms contract names and effects vocabulary | Burrow | none |
| 3. Burrow gate 6.2 with exact key, bundles 6.3, adversarial catalog 5.3 | Burrow | Burrow phase 3 done |
| 4. Gopher writer, `Extract`, edge-vector rules (plan tasks 9 to 11, plus 5.2) | Gopher | step 1 verdict |
| 5. Adapter and executor lookup with the gate | GopherMind | steps 2, 3, 4 |
| 6. Planner emits Gopher | GopherMind | steps 1, 4 |
| 7. Archive 7.1 | GopherMind | step 4 |
| 8. Import stage 7.3 | GopherMind | steps 5, 7 |
| 9. Embedding candidates and cross-check at scale | Burrow | Burrow phase 4, John's model decision |

Steps 1, 2 and 3 run in parallel.

---

## 10. Open questions

1. **`feat` record.** What it means in the original spec (`path=`, `dial`, `answers=`, `overrides=`). If it is meant for importing features, `reuse` folds into it. (John)
2. **`job=` on a leaf.** Its meaning, carried over from revision 1. (John)
3. **Burrow names.** Contract field names, effects vocabulary, type spelling. (Burrow)
4. **Archive location** on the server and how `<project>@latest` resolves. (GopherMind)
5. **Cross-check size.** 200 generated inputs is a starting value; Burrow may raise it if the catalog finds a twin that 200 misses.
6. **Burrow HTTP service and the phase 4 model.** Still waiting on John (ROADMAP section 7).

## 11. Decisions

- 2026-10-24, John: leaves use one Gopher syntax (Option B); `c=` removed.
- 2026-10-24, John: chains deferred; `chain=` reserved.
- 2026-10-24, John: 100% means zero false accepts; misses are acceptable.
- 2026-10-24, John: feature reuse from a previous project is explicit, named in the brief, with no search.
- 2026-10-25, GopherMind accepts Burrow's six implementation differences from the 4g gate (Burrow's 6.6): (1) no `key` field in the lookup request; (2) failure names compared lowercased and trimmed; (3) the type gate compares sorted multisets of input and output types and maps parameters by name, position or distinct type; (4) among agreeing candidates the exact-key one wins before smallest bundle; (5) the GopherMind executor calls with `param_order` and maps with `input_map` and `output_map` positions, not by name (section 4 is read this way); (6) a miss is not kept when the request is cancelled mid-gate or rejected with 400. None of them changes the zero false accept rule of section 5; they are recorded for John with the rest of the spec.
- 2026-10-25, GopherMind: `origin` of a leaf record maps from the gate answer as Burrow stated: a gate hit is `reused` (even with `awaiting_confirmation`), a miss with reason `not_eligible` is `not_eligible`, any other miss is `lookup_missed`, and a legacy (no vectors) answer is not the 100 percent path and is never used as a reuse.

## 12. Glossary

- **Eligible:** a leaf with `effects=none` and a verb and object; the only kind Burrow stores or returns.
- **Requester's vectors:** the `t` lines of the leaf asking for a function.
- **Bundle:** a stored root function with its pinned helpers, returned as one unit.
- **False accept:** Burrow returns a function that is not the one the requester's contract meant.
- **Import:** bringing records from an archived run's plan into a new plan, marked with `from=`.
