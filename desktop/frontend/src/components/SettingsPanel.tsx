import { useEffect, useState } from 'react'
import { ApiClient, type CatalogueEntry, type ModelSettings } from '../api/client'

/** knownTerms is the fixed vocabulary freellm.TermsFlags emits on the Go side. */
const knownTerms = ['non-commercial', 'trains on prompts', 'identity check']

/** entryKey is the "profile/model" key an entry is addressed by, matching internal/modelcat. */
function entryKey(e: CatalogueEntry): string {
  return `${e.profile}/${e.id}`
}

/** remoteEndpointStorageKey is where the (currently UI-only) remote endpoint choice lives. */
const remoteEndpointStorageKey = 'gophermind.remoteEndpoint'

interface RemoteEndpointDraft {
  useRemote: boolean
  baseURL: string
  token: string
}

function loadRemoteEndpointDraft(): RemoteEndpointDraft {
  try {
    const raw = localStorage.getItem(remoteEndpointStorageKey)
    if (raw) return JSON.parse(raw) as RemoteEndpointDraft
  } catch {
    // Ignore a corrupt or inaccessible value; fall through to the default.
  }
  return { useRemote: false, baseURL: '', token: '' }
}

interface SettingsPanelProps {
  client: ApiClient
  onClose: () => void
}

/**
 * SettingsPanel is every model-picker preference the spec lists, all
 * persisted through PATCH /models/settings: preference order, automatic
 * cycling, the capacity threshold, what to do when everything is full,
 * default filters, term exclusions and custom links.
 *
 * Two properties this panel keeps visible rather than implicit:
 *
 *   - excluding a term removes those providers from automatic cycling too,
 *     not just from the picker's list (internal/modelcat.Next enforces
 *     this; the copy here says so, since a filter that only affected
 *     display would defeat the one setting that exists for legal reasons).
 *   - an unreachable model states its exact remedy (the environment
 *     variable to set), so this panel doubles as the instructions for
 *     widening what is reachable.
 *
 * The endpoint row (embedded vs remote) is UI-only here: internal/serve's
 * desktop app always starts the embedded server today, and switching to a
 * remote one requires Go-side work (see docs/superpowers/specs/2026-09-10-
 * desktop-app-design.md) this phase's plan does not include. The choice is
 * kept in localStorage so the control is not simply missing from the
 * inventory, but flipping it has no effect on the running app yet.
 */
export default function SettingsPanel({ client, onClose }: SettingsPanelProps) {
  const [settings, setSettings] = useState<ModelSettings | null>(null)
  const [catalogue, setCatalogue] = useState<CatalogueEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [linkError, setLinkError] = useState<string | null>(null)
  const [linkScope, setLinkScope] = useState('')
  const [linkURL, setLinkURL] = useState('')
  const [remote, setRemote] = useState<RemoteEndpointDraft>(loadRemoteEndpointDraft)

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      try {
        const [s, c] = await Promise.all([client.getModelSettings(), client.getCatalogue()])
        if (cancelled) return
        setSettings(s)
        setCatalogue(c)
      } catch (err) {
        if (cancelled) return
        setError(err instanceof Error ? err.message : String(err))
      } finally {
        if (!cancelled) setLoading(false)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [client])

  /**
   * patch sends a partial settings change to the server and adopts its
   * response as the new local state. On failure (for example a rejected
   * custom link) the previous, known-good settings are kept: an optimistic
   * update that gets refused server-side must never look like it took.
   */
  async function patch(partial: Partial<ModelSettings>): Promise<boolean> {
    setError(null)
    try {
      const updated = await client.patchModelSettings(partial)
      setSettings(updated)
      return true
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
      return false
    }
  }

  function saveRemote(next: RemoteEndpointDraft) {
    setRemote(next)
    try {
      localStorage.setItem(remoteEndpointStorageKey, JSON.stringify(next))
    } catch {
      // Best effort only; this is a local UI convenience, not app state.
    }
  }

  if (loading) {
    return (
      <div className="settings-panel">
        <div className="settings-panel-header">
          <h2>model settings</h2>
          <button onClick={onClose}>close</button>
        </div>
        <div className="settings-panel-status">loading...</div>
      </div>
    )
  }

  if (!settings) {
    return (
      <div className="settings-panel">
        <div className="settings-panel-header">
          <h2>model settings</h2>
          <button onClick={onClose}>close</button>
        </div>
        <div className="settings-panel-status settings-panel-error">{error || 'settings unavailable'}</div>
      </div>
    )
  }

  const order = settings.order ?? []
  const excluded = new Set(settings.excluded_terms ?? [])
  const availableToAdd = catalogue.filter((e) => !order.includes(entryKey(e)))
  const unreachable = catalogue.filter((e) => !e.reachable && e.reason)
  // Only one row per profile is worth showing here: every model on an
  // unreachable profile fails for the same reason (the same env var).
  const unreachableByProfile = new Map<string, CatalogueEntry>()
  for (const e of unreachable) {
    if (!unreachableByProfile.has(e.profile)) unreachableByProfile.set(e.profile, e)
  }

  function moveOrder(i: number, dir: -1 | 1) {
    const next = [...order]
    const j = i + dir
    if (j < 0 || j >= next.length) return
    ;[next[i], next[j]] = [next[j], next[i]]
    void patch({ order: next })
  }

  function removeFromOrder(key: string) {
    void patch({ order: order.filter((k) => k !== key) })
  }

  function addToOrder(key: string) {
    if (!key || order.includes(key)) return
    void patch({ order: [...order, key] })
  }

  function toggleExcludedTerm(term: string) {
    const next = new Set(excluded)
    if (next.has(term)) next.delete(term)
    else next.add(term)
    void patch({ excluded_terms: [...next] })
  }

  async function saveCustomLink() {
    const scope = linkScope.trim()
    const url = linkURL.trim()
    if (!scope || !url) return
    setLinkError(null)
    const ok = await patch({ custom_links: { ...(settings?.custom_links ?? {}), [scope]: url } })
    if (ok) {
      setLinkURL('')
    } else {
      setLinkError(error)
    }
  }

  function removeCustomLink(key: string) {
    const next = { ...(settings?.custom_links ?? {}) }
    delete next[key]
    void patch({ custom_links: next })
  }

  return (
    <div className="settings-panel">
      <div className="settings-panel-header">
        <h2>model settings</h2>
        <button onClick={onClose}>close</button>
      </div>

      {error && <div className="settings-panel-status settings-panel-error">{error}</div>}

      <section>
        <h3>preference order</h3>
        <p className="settings-panel-hint">
          Cycling moves down this list when the active model nears capacity. Use up/down to reorder.
        </p>
        <ul className="settings-order-list">
          {order.map((key, i) => (
            <li key={key}>
              <span>{key}</span>
              <button onClick={() => moveOrder(i, -1)} disabled={i === 0}>
                up
              </button>
              <button onClick={() => moveOrder(i, 1)} disabled={i === order.length - 1}>
                down
              </button>
              <button onClick={() => removeFromOrder(key)}>remove</button>
            </li>
          ))}
          {order.length === 0 && <li className="settings-panel-hint">no preference set; catalogue order is used</li>}
        </ul>
        {availableToAdd.length > 0 && (
          <select value="" onChange={(e) => addToOrder(e.target.value)}>
            <option value="">add a model to your preference order...</option>
            {availableToAdd.map((e) => (
              <option key={entryKey(e)} value={entryKey(e)}>
                {entryKey(e)}
              </option>
            ))}
          </select>
        )}
      </section>

      <section>
        <h3>automatic cycling</h3>
        <label>
          <input
            type="checkbox"
            checked={settings.cycle_on_capacity}
            onChange={(e) => void patch({ cycle_on_capacity: e.target.checked })}
          />
          cycle to the next preferred model automatically when the active one nears capacity
        </label>
      </section>

      <section>
        <h3>capacity threshold</h3>
        <label>
          <input
            type="range"
            min={50}
            max={99}
            value={settings.capacity_percent || 90}
            onChange={(e) => void patch({ capacity_percent: Number(e.target.value) })}
          />
          {settings.capacity_percent || 90}%
        </label>
      </section>

      <section>
        <h3>when everything is full</h3>
        <label>
          <input
            type="radio"
            name="when-all-full"
            checked={(settings.when_all_full || 'stay') === 'stay'}
            onChange={() => void patch({ when_all_full: 'stay' })}
          />
          stay on the current model
        </label>
        <label>
          <input
            type="radio"
            name="when-all-full"
            checked={settings.when_all_full === 'ask'}
            onChange={() => void patch({ when_all_full: 'ask' })}
          />
          stop and ask
        </label>
      </section>

      <section>
        <h3>default filters</h3>
        <label>
          <input
            type="checkbox"
            checked={settings.filter_reachable}
            onChange={(e) => void patch({ filter_reachable: e.target.checked })}
          />
          picker opens showing only usable-now models
        </label>
        <label>
          <input
            type="checkbox"
            checked={settings.filter_has_capacity}
            onChange={(e) => void patch({ filter_has_capacity: e.target.checked })}
          />
          picker opens hiding models with no capacity left
        </label>
      </section>

      <section>
        <h3>excluded terms</h3>
        <p className="settings-panel-hint">
          An excluded term removes those providers from automatic cycling entirely, not just from the
          picker's list - this is the setting to rely on for legal or contractual constraints.
        </p>
        {knownTerms.map((term) => (
          <label key={term}>
            <input type="checkbox" checked={excluded.has(term)} onChange={() => toggleExcludedTerm(term)} />
            {term}
          </label>
        ))}
      </section>

      <section>
        <h3>custom links</h3>
        <p className="settings-panel-hint">
          A scope is a provider's profile name (e.g. "free-groq") or a model key ("free-groq/openai/gpt-oss-120b").
          Only http and https URLs are accepted; the server rejects anything else.
        </p>
        <ul className="settings-links-list">
          {Object.entries(settings.custom_links ?? {}).map(([key, url]) => (
            <li key={key}>
              <span className="settings-links-key">{key}</span>
              <a href={url} target="_blank" rel="noreferrer">
                {url}
              </a>
              <button onClick={() => removeCustomLink(key)}>remove</button>
            </li>
          ))}
        </ul>
        <div className="settings-links-add">
          <input placeholder="scope (profile or profile/model)" value={linkScope} onChange={(e) => setLinkScope(e.target.value)} />
          <input placeholder="https://..." value={linkURL} onChange={(e) => setLinkURL(e.target.value)} />
          <button onClick={() => void saveCustomLink()}>save link</button>
        </div>
        {linkError && <div className="settings-panel-status settings-panel-error">{linkError}</div>}
      </section>

      {unreachableByProfile.size > 0 && (
        <section>
          <h3>unreachable providers</h3>
          <ul className="settings-unreachable-list">
            {[...unreachableByProfile.values()].map((e) => (
              <li key={e.profile}>
                <span className="settings-links-key">{e.provider || e.profile}</span>
                <span>{e.reason}</span>
              </li>
            ))}
          </ul>
        </section>
      )}

      <section>
        <h3>endpoint</h3>
        <p className="settings-panel-hint">
          Embedded is the default and what this build actually uses. Remote is not yet wired to the running
          app; saving it here only remembers the choice for when that support lands.
        </p>
        <label>
          <input
            type="radio"
            name="endpoint-mode"
            checked={!remote.useRemote}
            onChange={() => saveRemote({ ...remote, useRemote: false })}
          />
          embedded
        </label>
        <label>
          <input
            type="radio"
            name="endpoint-mode"
            checked={remote.useRemote}
            onChange={() => saveRemote({ ...remote, useRemote: true })}
          />
          remote
        </label>
        {remote.useRemote && (
          <div className="settings-remote-fields">
            <input
              placeholder="https://host:port"
              value={remote.baseURL}
              onChange={(e) => saveRemote({ ...remote, baseURL: e.target.value })}
            />
            <input
              placeholder="bearer token"
              value={remote.token}
              onChange={(e) => saveRemote({ ...remote, token: e.target.value })}
            />
          </div>
        )}
      </section>
    </div>
  )
}
