import { useEffect, useMemo, useState } from 'react'
import { ApiClient, type CatalogueEntry, type ModelSettings } from '../api/client'

/** knownTerms is the fixed vocabulary freellm.TermsFlags emits on the Go side. */
const knownTerms = ['non-commercial', 'trains on prompts', 'identity check']

/** entryKey is the "profile/model" key an entry is addressed by, matching internal/modelcat. */
function entryKey(e: CatalogueEntry): string {
  return `${e.profile}/${e.id}`
}

/**
 * windowAbbrev and unitAbbrev mirror internal/freellm's Quota.String(): a
 * denominator is rendered as "312/1,000 RPD", never invented for a model
 * with no published quota.
 */
function windowAbbrev(window?: string): string {
  switch (window) {
    case 'minute':
      return 'PM'
    case 'hour':
      return 'P/hr'
    case 'day':
      return 'PD'
    case 'month':
      return 'PMo'
    default:
      return 'P?'
  }
}

function unitAbbrev(unit?: string): string {
  return unit === 'tokens' ? 'T' : 'R'
}

/**
 * allowanceText renders one entry's remaining allowance. A published quota
 * shows the fraction; its absence shows a bare count, never a fraction -
 * the rule this whole feature depends on to be trusted.
 */
function allowanceText(e: CatalogueEntry): string {
  if (e.quota > 0) {
    return `${e.used.toLocaleString()}/${e.quota.toLocaleString()} ${unitAbbrev(e.unit)}${windowAbbrev(e.window)}`
  }
  const unit = e.unit === 'tokens' ? 'tokens' : 'requests'
  return `${e.used.toLocaleString()} ${unit}`
}

interface ModelPickerProps {
  client: ApiClient
  /** currentProfile/currentModel highlight the row the active session is using. */
  currentProfile: string
  currentModel: string
}

/**
 * ModelPicker is the dropdown listing every model gophermind can address:
 * name, provider and remaining allowance, filterable by reachability,
 * capacity, terms and provider/modality, each linking its provider and
 * model page. It fetches GET /models/catalogue and GET /models/settings
 * (for the filters' defaults) fresh every time it opens, so it never shows
 * stale usage.
 *
 * This is a view: picking a model here does not change the running
 * session, since internal/serve has no route to change a session's model
 * mid-run (only at session creation). Deliberate steering happens through
 * the preference order in SettingsPanel, which both automatic cycling and
 * a future session honor.
 */
export default function ModelPicker({ client, currentProfile, currentModel }: ModelPickerProps) {
  const [open, setOpen] = useState(false)
  const [entries, setEntries] = useState<CatalogueEntry[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const [filterReachable, setFilterReachable] = useState(true)
  const [filterHasCapacity, setFilterHasCapacity] = useState(false)
  const [hiddenTerms, setHiddenTerms] = useState<Set<string>>(new Set())
  const [providerFilter, setProviderFilter] = useState('')
  const [modalityFilter, setModalityFilter] = useState('')

  useEffect(() => {
    if (!open) return
    let cancelled = false
    setLoading(true)
    setError(null)
    ;(async () => {
      try {
        const [catalogue, settings] = await Promise.all([client.getCatalogue(), client.getModelSettings()])
        if (cancelled) return
        setEntries(catalogue)
        applySettingsDefaults(settings)
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
  }, [open, client])

  function applySettingsDefaults(s: ModelSettings) {
    setFilterReachable(s.filter_reachable)
    setFilterHasCapacity(s.filter_has_capacity)
    setHiddenTerms(new Set(s.excluded_terms ?? []))
  }

  const providers = useMemo(() => {
    const set = new Set<string>()
    for (const e of entries) if (e.provider) set.add(e.provider)
    return [...set].sort()
  }, [entries])

  const modalities = useMemo(() => {
    const set = new Set<string>()
    for (const e of entries) if (e.modality) set.add(e.modality)
    return [...set].sort()
  }, [entries])

  const filtered = useMemo(() => {
    return entries.filter((e) => {
      if (filterReachable && !e.reachable) return false
      if (filterHasCapacity && e.near_capacity) return false
      if (e.terms?.some((t) => hiddenTerms.has(t))) return false
      if (providerFilter && e.provider !== providerFilter) return false
      if (modalityFilter && e.modality !== modalityFilter) return false
      return true
    })
  }, [entries, filterReachable, filterHasCapacity, hiddenTerms, providerFilter, modalityFilter])

  function toggleHiddenTerm(term: string) {
    setHiddenTerms((prev) => {
      const next = new Set(prev)
      if (next.has(term)) next.delete(term)
      else next.add(term)
      return next
    })
  }

  const currentLabel = currentModel
    ? `${currentModel}${currentProfile ? ` (${currentProfile})` : ''}`
    : 'choose a model'

  return (
    <div className="model-picker">
      <button className="model-picker-toggle" onClick={() => setOpen((v) => !v)}>
        {currentLabel}
        <span className="model-picker-caret">{open ? '▴' : '▾'}</span>
      </button>

      {open && (
        <div className="model-picker-panel">
          <div className="model-picker-filters">
            <label>
              <input
                type="checkbox"
                checked={filterReachable}
                onChange={(e) => setFilterReachable(e.target.checked)}
              />
              usable now
            </label>
            <label>
              <input
                type="checkbox"
                checked={filterHasCapacity}
                onChange={(e) => setFilterHasCapacity(e.target.checked)}
              />
              has capacity left
            </label>
            <select value={providerFilter} onChange={(e) => setProviderFilter(e.target.value)}>
              <option value="">all providers</option>
              {providers.map((p) => (
                <option key={p} value={p}>
                  {p}
                </option>
              ))}
            </select>
            <select value={modalityFilter} onChange={(e) => setModalityFilter(e.target.value)}>
              <option value="">all modalities</option>
              {modalities.map((m) => (
                <option key={m} value={m}>
                  {m}
                </option>
              ))}
            </select>
          </div>
          <div className="model-picker-terms">
            {knownTerms.map((term) => (
              <label key={term}>
                <input
                  type="checkbox"
                  checked={hiddenTerms.has(term)}
                  onChange={() => toggleHiddenTerm(term)}
                />
                hide {term}
              </label>
            ))}
          </div>

          {loading && <div className="model-picker-status">loading catalogue...</div>}
          {error && <div className="model-picker-status model-picker-error">{error}</div>}

          <div className="model-picker-list">
            {filtered.map((e) => {
              const isCurrent = e.profile === currentProfile && e.id === currentModel
              return (
                <div
                  key={entryKey(e)}
                  className={`model-picker-row${isCurrent ? ' model-picker-row-current' : ''}${e.reachable ? '' : ' model-picker-row-unreachable'}`}
                >
                  <span className="model-picker-id">{e.id}</span>
                  <span className="model-picker-provider">{e.provider || 'your endpoint'}</span>
                  <span className="model-picker-allowance">
                    {e.reachable ? allowanceText(e) : e.reason || 'not reachable'}
                  </span>
                  {e.terms && e.terms.length > 0 && (
                    <span className="model-picker-terms-badges">{e.terms.join(', ')}</span>
                  )}
                  <span className="model-picker-links">
                    {e.provider_url && (
                      <a href={e.provider_url} target="_blank" rel="noreferrer">
                        provider
                      </a>
                    )}
                    {e.model_url && (
                      <a href={e.model_url} target="_blank" rel="noreferrer">
                        model
                      </a>
                    )}
                  </span>
                </div>
              )
            })}
            {!loading && filtered.length === 0 && (
              <div className="model-picker-status">no models match these filters</div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
