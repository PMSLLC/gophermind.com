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

/** StreamHandlers are called as SSE frames arrive from a session turn. */
export interface StreamHandlers {
  onToken: (text: string) => void
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
