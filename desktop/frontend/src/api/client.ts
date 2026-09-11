// client.ts is a small typed client over the internal/serve routes this
// task's Chat screen uses: POST /session and POST /session/{id}/stream.
//
// EventSource cannot send a POST body or an Authorization header, so the
// streaming turn uses fetch with a ReadableStream reader and a hand-rolled
// SSE frame parser instead. This is expected - the desktop app spec's
// mention of EventSource describes the streaming model, not the literal API
// used to consume it.

/** SessionInfo is the response body of POST /session. */
export interface SessionInfo {
  id: string
}

/**
 * BackendStatus is the response body of GET /backend-status: which LLM
 * endpoint and model the embedded server ended up using, whether that took
 * a fallback away from the configured endpoint, and the failure text when
 * even the fallback did not work. ready is false while resolution is still
 * in flight (error will also be empty in that case).
 */
export interface BackendStatus {
  ready: boolean
  baseURL: string
  model: string
  fellBack: boolean
  failedBaseURL?: string
  fallbackProfile?: string
  error?: string
}

/** SSEFrame is one parsed "event: ...\ndata: ...\n\n" block. */
export interface SSEFrame {
  event: string
  data: string
}

/**
 * CatalogueEntry mirrors internal/modelcat.Entry: one model gophermind can
 * address, with reachability, remaining allowance and links. quota is
 * absent (rather than 0) when the provider publishes no quota, matching the
 * Go side's own "a bare count, never a fraction" rule.
 */
export interface CatalogueEntry {
  id: string
  provider: string
  profile: string
  reachable: boolean
  reason?: string
  used: number
  quota: number
  unit?: string
  window?: string
  context?: string
  modality?: string
  terms?: string[]
  provider_url?: string
  model_url?: string
  near_capacity: boolean
}

/**
 * ModelSettings mirrors internal/modelcat.Settings: the user's preference
 * order, capacity threshold, term exclusions, filter defaults and custom
 * links, persisted server-side so they follow the user between clients.
 */
export interface ModelSettings {
  order?: string[]
  cycle_on_capacity: boolean
  capacity_percent: number
  when_all_full?: string
  filter_reachable: boolean
  filter_has_capacity: boolean
  excluded_terms?: string[]
  custom_links?: Record<string, string>
}

/**
 * ModelSwitched is the payload of a "model-switched" SSE frame: the model
 * picker's policy moved the active model before this turn started.
 */
export interface ModelSwitched {
  profile: string
  model: string
  reason: string
}

/**
 * PendingApproval is the payload of an "approval-needed" SSE frame: a gated
 * tool call is blocked on the server, waiting for POST
 * /session/{id}/approve to resolve approvalID.
 */
export interface PendingApproval {
  approvalID: string
  tool: string
  args: string
}

/**
 * ApprovalResolution is what resolveApproval reports back: alreadyResolved
 * true means the server returned 404 for this approval id (it was unknown
 * or someone else already answered it), which is not a failure.
 */
export interface ApprovalResolution {
  alreadyResolved: boolean
}

/** StreamHandlers are called as SSE frames arrive from a session turn. */
export interface StreamHandlers {
  onToken: (text: string) => void
  onApprovalNeeded: (approval: PendingApproval) => void
  onModelSwitched?: (switched: ModelSwitched) => void
  onDone: () => void
  onError: (message: string) => void
}

/** ApiClient talks to one embedded or remote GopherMind server instance. */
export class ApiClient {
  constructor(
    private readonly baseURL: string,
    private readonly token: string,
  ) {}

  private authHeaders(extra?: Record<string, string>): Record<string, string> {
    return { Authorization: `Bearer ${this.token}`, ...extra }
  }

  /** createSession calls POST /session and returns the new session's id. */
  async createSession(): Promise<SessionInfo> {
    const res = await fetch(`${this.baseURL}/session`, {
      method: 'POST',
      headers: this.authHeaders(),
    })
    if (!res.ok) {
      throw new Error(`create session failed: ${res.status} ${await safeText(res)}`)
    }
    return (await res.json()) as SessionInfo
  }

  /**
   * streamTurn posts task to POST /session/{id}/stream and parses the SSE
   * response as it arrives, calling handlers.onToken for each "token" frame
   * and handlers.onDone / handlers.onError for the terminal frames. Resolves
   * once the response body ends.
   */
  async streamTurn(sessionID: string, task: string, handlers: StreamHandlers): Promise<void> {
    const res = await fetch(`${this.baseURL}/session/${encodeURIComponent(sessionID)}/stream`, {
      method: 'POST',
      headers: this.authHeaders({ 'Content-Type': 'text/plain' }),
      body: task,
    })
    if (!res.ok || !res.body) {
      handlers.onError(`stream request failed: ${res.status} ${await safeText(res)}`)
      return
    }

    const reader = res.body.getReader()
    const decoder = new TextDecoder()
    let buf = ''

    const dispatch = (frame: SSEFrame) => {
      switch (frame.event) {
        case 'token':
          handlers.onToken(frame.data)
          break
        case 'approval-needed': {
          const payload = JSON.parse(frame.data) as {
            approval_id: string
            tool: string
            args: string
          }
          handlers.onApprovalNeeded({
            approvalID: payload.approval_id,
            tool: payload.tool,
            args: payload.args,
          })
          break
        }
        case 'model-switched':
          if (handlers.onModelSwitched) {
            handlers.onModelSwitched(JSON.parse(frame.data) as ModelSwitched)
          }
          break
        case 'done':
          handlers.onDone()
          break
        case 'error':
          handlers.onError(frame.data || 'run failed')
          break
        default:
          // Other event types (assistant, tool_call, tool_result, usage) are
          // not rendered by this task's Chat screen; later screens consume
          // them.
          break
      }
    }

    for (;;) {
      const { value, done } = await reader.read()
      if (done) break
      buf += decoder.decode(value, { stream: true })

      let sep: number
      while ((sep = buf.indexOf('\n\n')) !== -1) {
        const rawFrame = buf.slice(0, sep)
        buf = buf.slice(sep + 2)
        const frame = parseSSEFrame(rawFrame)
        if (frame) dispatch(frame)
      }
    }
  }

  /**
   * resolveApproval posts a human decision to POST /session/{id}/approve
   * for one pending gated tool call. A 404 response means the approval id
   * is unknown or was already resolved - the contract calls that "someone
   * already answered this", not a failure, so this reports it via
   * alreadyResolved instead of throwing.
   */
  async resolveApproval(
    sessionID: string,
    approvalID: string,
    approved: boolean,
  ): Promise<ApprovalResolution> {
    const res = await fetch(`${this.baseURL}/session/${encodeURIComponent(sessionID)}/approve`, {
      method: 'POST',
      headers: this.authHeaders({ 'Content-Type': 'application/json' }),
      body: JSON.stringify({ approval_id: approvalID, approved }),
    })
    if (res.status === 404) {
      return { alreadyResolved: true }
    }
    if (!res.ok) {
      throw new Error(`resolve approval failed: ${res.status} ${await safeText(res)}`)
    }
    return { alreadyResolved: false }
  }

  /** getBackendStatus calls GET /backend-status. */
  async getBackendStatus(): Promise<BackendStatus> {
    const res = await fetch(`${this.baseURL}/backend-status`, {
      method: 'GET',
      headers: this.authHeaders(),
    })
    if (!res.ok) {
      throw new Error(`backend status failed: ${res.status} ${await safeText(res)}`)
    }
    return (await res.json()) as BackendStatus
  }

  /** listModels calls GET /models. */
  async listModels(): Promise<string[]> {
    const res = await fetch(`${this.baseURL}/models`, {
      method: 'GET',
      headers: this.authHeaders(),
    })
    if (!res.ok) {
      throw new Error(`list models failed: ${res.status} ${await safeText(res)}`)
    }
    const body = (await res.json()) as { models: string[] }
    return body.models
  }

  /** getCatalogue calls GET /models/catalogue: every model gophermind knows about. */
  async getCatalogue(): Promise<CatalogueEntry[]> {
    const res = await fetch(`${this.baseURL}/models/catalogue`, {
      method: 'GET',
      headers: this.authHeaders(),
    })
    if (!res.ok) {
      throw new Error(`get catalogue failed: ${res.status} ${await safeText(res)}`)
    }
    const body = (await res.json()) as { entries: CatalogueEntry[] }
    return body.entries
  }

  /** getModelSettings calls GET /models/settings. */
  async getModelSettings(): Promise<ModelSettings> {
    const res = await fetch(`${this.baseURL}/models/settings`, {
      method: 'GET',
      headers: this.authHeaders(),
    })
    if (!res.ok) {
      throw new Error(`get model settings failed: ${res.status} ${await safeText(res)}`)
    }
    return (await res.json()) as ModelSettings
  }

  /**
   * patchModelSettings calls PATCH /models/settings with a partial settings
   * object. A 400 response (an invalid custom link scheme, named in the
   * body) is thrown as an Error carrying that exact server text rather than
   * a generic message: the caller shows it inline instead of re-deriving
   * its own validation, which the server is the only real authority on.
   */
  async patchModelSettings(patch: Partial<ModelSettings>): Promise<ModelSettings> {
    const res = await fetch(`${this.baseURL}/models/settings`, {
      method: 'PATCH',
      headers: this.authHeaders({ 'Content-Type': 'application/json' }),
      body: JSON.stringify(patch),
    })
    if (!res.ok) {
      throw new Error(await safeText(res))
    }
    return (await res.json()) as ModelSettings
  }
}

/**
 * parseSSEFrame parses one "event: X\ndata: Y\ndata: Z" block (without the
 * trailing blank line) into an SSEFrame. Multiple "data:" lines are joined
 * with "\n", matching the SSE spec and internal/serve's writeSSEEvent, which
 * splits multi-line payloads across several "data:" lines. Returns null for
 * an empty block (e.g. keep-alive).
 */
export function parseSSEFrame(raw: string): SSEFrame | null {
  const lines = raw.split('\n')
  let event = 'message'
  const dataLines: string[] = []
  for (const line of lines) {
    if (line.startsWith('event: ')) {
      event = line.slice('event: '.length)
    } else if (line.startsWith('data: ')) {
      dataLines.push(line.slice('data: '.length))
    } else if (line === 'data:') {
      dataLines.push('')
    }
  }
  if (dataLines.length === 0 && event === 'message') return null
  return { event, data: dataLines.join('\n') }
}

async function safeText(res: Response): Promise<string> {
  try {
    return await res.text()
  } catch {
    return ''
  }
}
