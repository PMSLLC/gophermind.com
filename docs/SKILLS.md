# Skills

A skill is a Markdown capability pack injected into an agent's system prompt.
Two kinds, with deliberately different trust.

## Repo-local packs, always on

`<repo>/.gophermind/skills/*.md` are injected on every turn. They were
committed to the project, which is the same consent `CLAUDE.md` carries, so
they have no toggle. `README.md` there is documentation and is not injected.

Every file is concatenated into every turn's system prompt, and the total is
capped at a quarter of the model's context window alongside the persona, repo
instructions and repo map. Anything most turns do not use is better as a tool:
`humanize` used to live here and cost ~7k tokens on every coding turn.

## Fetched sources, off until enabled

Managed from the desktop settings panel, or over HTTP:

```
GET    /skills                 catalogue with enablement state
PATCH  /skills                 {"key":"owner/repo:skill","enabled":true}
POST   /skills/sources         {"url":"https://github.com/owner/repo"}
DELETE /skills/sources/{id}
```

Settings live in `~/.gophermind/skills.json`, content in
`~/.gophermind/skill-cache/<owner>/<repo>@<sha>/`.

### Why it works this way

A skill is instructions for an agent that runs shell commands and edits files,
and during an unattended run those tools are auto-approved. A skill fetched from
a repository you do not control is remote prompt injection with a supply chain
attached. Three rules follow.

**Nothing fetched is enabled by arriving on disk.** Adding a source makes its
content reviewable. Switching any of it on is a separate act.

**Sources pin a commit, never a branch.** A repository can be rewritten after it
was reviewed, so a floating ref would give "I read these skills" a shelf life of
zero.

**Enablement is scoped to its source** (`owner/repo:skill`), so a newly added
repository cannot shadow a skill you already trusted by reusing its name.

The clone is inert: hooks disabled, submodules refused, `ext::` transport
refused, depth one, `GIT_*` stripped, and `.git` removed before install so the
cache holds reviewed files at a known commit rather than a working repository.

Read a skill before enabling it. That is the control; the rest is plumbing to
make sure you get the chance.
