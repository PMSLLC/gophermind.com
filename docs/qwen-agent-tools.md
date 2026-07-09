# Tools Qwen Uses — Qwen-Agent built-in registry (checklist)

A model (e.g. `qwen3.6-35b-a3b`) has no hard-wired tools of its own — the harness
registers them and the model calls whatever it's given. What Qwen's function-calling
is **trained and documented around** is the [Qwen-Agent](https://github.com/QwenLM/Qwen-Agent)
`TOOL_REGISTRY` plus its extension mechanisms (MCP servers, `@register_tool`).
That canonical set is listed below.

Checkboxes = implementation status **in gophermind**. Each item notes the gophermind
analog so this doubles as a tool roadmap. (Source: Qwen-Agent docs via Context7,
2026-07. Framework-level list, not a version-specific "3.6" manifest.)

---

## A. Code execution

- [ ] **`code_interpreter`** — execute Python in a Docker-sandboxed kernel; the workhorse for calculation, data wrangling, plotting. _gophermind: partial — `run_shell` can invoke python, but there's no dedicated stateful/sandboxed code tool._
- [ ] **`python_executor`** — run Python **in-process** (no Docker), lighter weight; used by Qwen's `TIRMathAgent` for tool-integrated-reasoning math. _gophermind: none (covered loosely by `run_shell`)._

## B. Web

- [ ] **`web_search`** — search the web and return ranked results. _gophermind: none._
- [ ] **`web_extractor`** — fetch a URL and extract its readable content. _gophermind: none — this is the `fetch_url` tool already on the roadmap._

## C. Documents & retrieval (RAG)

- [ ] **`retrieval`** — RAG over user-provided files: chunk, embed, and retrieve relevant passages into context. _gophermind: none (backlog: repo retrieval)._
- [ ] **`doc_parser`** — structured parsing of documents (PDF/Word/etc.) into text + metadata. _gophermind: none — `read_file` handles plain text only._
- [ ] **`simple_doc_parser`** — lightweight/fast document-to-text parsing (fewer deps than `doc_parser`). _gophermind: none._

## D. Vision & images

- [ ] **`image_gen`** — text-to-image generation, returns an image URL. _gophermind: out of scope (no vision/media path)._
- [ ] **`image_search`** — search for images by query. _gophermind: out of scope._
- [ ] **`image_zoom_in_tool`** — crop/zoom into a region of an image for closer inspection (agentic vision). _gophermind: out of scope._

## E. Utility & memory

- [ ] **`storage`** — key/value persistence the agent can read/write across turns (durable scratch state / memory). _gophermind: none (backlog: pinned scratchpad)._
- [ ] **`weather`** — example configurable tool (needs an API key, e.g. OpenWeather/amap); ships as the canonical "external API" demo. _gophermind: n/a (example only)._

## F. Extensibility mechanisms (how Qwen gets *everything else*)

- [ ] **MCP servers** — attach any Model Context Protocol server (`time`, `filesystem`, `fetch`, …) via `function_list` config; the model uses those tools transparently. _gophermind: none (backlog: MCP client)._
- [ ] **`@register_tool` custom tools** — register a `BaseTool` with name/description/params + a `call()` impl. _gophermind: this is exactly gophermind's `Tool{Name, Description, Schema, Run}` abstraction — the analog already exists._

---

## Cross-reference: gophermind's own tools vs. the Qwen surface

gophermind already ships file/shell tools that, in a Qwen-Agent setup, you'd get from
`mcp-server-filesystem` or custom `@register_tool` tools:

| gophermind tool | Qwen-Agent equivalent |
|---|---|
| `read_file` | custom / `mcp-server-filesystem` (+ `doc_parser` for non-text) |
| `write_file`, `edit_file` | custom / `mcp-server-filesystem` |
| `list_files` | custom / `mcp-server-filesystem` |
| `search` (rg/grep) | custom tool |
| `run_shell` | `code_interpreter` / `python_executor` (Qwen leans on Python, not shell) |

**Biggest gaps vs. what Qwen expects to reach for:** `web_search` + `web_extractor`
(web access), `retrieval` (RAG), `doc_parser` (non-text files), `storage` (durable
memory), and an **MCP client** — which would let gophermind borrow the entire MCP
tool ecosystem in one move instead of porting each tool.
