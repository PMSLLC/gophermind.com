import { useEffect, useState } from 'react'
import type { ApiClient, SkillCatalogue, SkillInfo } from '../api/client'

interface SkillsPanelProps {
  client: ApiClient
}

/**
 * SkillsPanel manages capability packs: which repositories are installed, and
 * which of their skills are switched on.
 *
 * The security story this renders, because the UI is where it has to be
 * legible: a skill is instructions for an agent that runs shell commands, so a
 * skill from a fetched repository is untrusted text until someone reads it.
 * Adding a source therefore installs content and enables none of it, and each
 * source shows the commit it is pinned to, since a repository can be rewritten
 * after it was reviewed.
 *
 * A pack committed to this repo is always on and has no toggle. Its consent is
 * the commit, the same way CLAUDE.md's is, and offering a switch that does
 * nothing on the next run would be worse than offering none.
 */
export default function SkillsPanel({ client }: SkillsPanelProps) {
  const [cat, setCat] = useState<SkillCatalogue | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [url, setUrl] = useState('')

  async function refresh() {
    try {
      // Normalise before storing. Go marshals a nil slice as null, and this
      // panel iterates both fields; a null here took the whole settings
      // screen down rather than showing an empty list. The server no longer
      // sends null, but a panel that cannot survive one is a panel that
      // breaks again the next time some other endpoint does.
      const got = await client.getSkills()
      setCat({ sources: got?.sources ?? [], skills: got?.skills ?? [] })
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  useEffect(() => {
    void refresh()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  async function toggle(s: SkillInfo, on: boolean) {
    setBusy(true)
    try {
      await client.setSkillEnabled(s.key, on)
      await refresh()
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  async function addSource() {
    const trimmed = url.trim()
    if (!trimmed) return
    setBusy(true)
    try {
      await client.addSkillSource(trimmed)
      setUrl('')
      await refresh()
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  async function removeSource(id: string) {
    setBusy(true)
    try {
      await client.removeSkillSource(id)
      await refresh()
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  if (!cat) {
    return (
      <section>
        <h3>skills</h3>
        <p className="hint">{error ?? 'loading...'}</p>
      </section>
    )
  }

  const repoLocal = cat.skills.filter((s) => !s.source)
  const bySource = new Map<string, SkillInfo[]>()
  for (const s of cat.skills) {
    if (!s.source) continue
    bySource.set(s.source, [...(bySource.get(s.source) ?? []), s])
  }

  function sourceID(u: string): string {
    const parts = u.replace(/\.git$/, '').replace(/\/+$/, '').split('/')
    return parts.length >= 2 ? `${parts[parts.length - 2]}/${parts[parts.length - 1]}` : u
  }

  return (
    <section className="skills">
      <h3>skills</h3>
      {error && <p className="skills-error">{error}</p>}

      <p className="hint">
        A skill is instructions for an agent that can run shell commands. Adding
        a source installs its content and switches nothing on; read a skill
        before enabling it.
      </p>

      <div className="skills-add">
        <input
          type="text"
          placeholder="https://github.com/owner/repo"
          value={url}
          disabled={busy}
          onChange={(e) => setUrl(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault()
              void addSource()
            }
          }}
        />
        <button disabled={busy || !url.trim()} onClick={() => void addSource()}>
          add source
        </button>
      </div>

      {repoLocal.length > 0 && (
        <div className="skills-group">
          <div className="skills-group-head">
            this repo <span className="skills-note">always on</span>
          </div>
          {repoLocal.map((s) => (
            <div key={s.key} className="skill">
              <div className="skill-name">{s.name}</div>
              {s.description && <div className="skill-desc">{s.description}</div>}
            </div>
          ))}
        </div>
      )}

      {cat.sources.map((src) => {
        const id = sourceID(src.url)
        const list = bySource.get(id) ?? []
        return (
          <div key={src.url} className="skills-group">
            <div className="skills-group-head">
              {id}
              {/* The pinned commit, shown because it is the thing that makes
                  "I reviewed this" mean anything: upstream can move, this
                  cannot without an explicit update. */}
              <span className="skills-note" title={src.sha}>
                pinned {src.sha.slice(0, 12)}
              </span>
              <button
                className="skills-remove"
                disabled={busy}
                onClick={() => void removeSource(id)}
              >
                remove
              </button>
            </div>
            {list.length === 0 && <div className="hint">no skills found in this source</div>}
            {list.map((s) => (
              <label key={s.key} className="skill skill-toggle">
                <input
                  type="checkbox"
                  checked={s.enabled}
                  disabled={busy}
                  onChange={(e) => void toggle(s, e.target.checked)}
                />
                <span>
                  <span className="skill-name">{s.name}</span>
                  {s.description && <span className="skill-desc">{s.description}</span>}
                </span>
              </label>
            ))}
          </div>
        )
      })}

      {cat.sources.length === 0 && (
        <p className="hint">No skill sources installed.</p>
      )}
    </section>
  )
}
