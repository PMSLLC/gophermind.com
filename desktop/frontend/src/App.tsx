import { useEffect, useRef, useState } from 'react'
import { ApiClient, type BackendStatus } from './api/client'
import { getEndpoint } from './api/wails'

interface TranscriptLine {
  role: 'user' | 'assistant' | 'system'
  text: string
}

type Status = 'connecting' | 'ready' | 'sending' | 'error'

// backendPollTimeoutMs bounds how long the app waits for GET /backend-status
// to report ready or error before giving up and showing a timeout error
// itself. Backend resolution (see desktop/backend.go's resolveLLMBackend)
// is bounded by a short liveness probe on the configured endpoint, plus at
// most one more on the free-provider fallback, so it should finish in single
// digit seconds; this is a generous ceiling, not the expected case.
const backendPollTimeoutMs = 30000

/**
 * waitForBackend polls GET /backend-status until it reports ready or a
 * fatal error, or backendPollTimeoutMs elapses. stopped is checked between
 * polls so it stops promptly if the component unmounts.
 */
async function waitForBackend(client: ApiClient, stopped: () => boolean): Promise<BackendStatus> {
  const deadline = Date.now() + backendPollTimeoutMs
  for (;;) {
    if (stopped()) {
      return { ready: false, baseURL: '', model: '', fellBack: false }
    }
    const s = await client.getBackendStatus()
    if (s.ready || s.error) return s
    if (Date.now() > deadline) {
      return {
        ready: false,
        baseURL: '',
        model: '',
        fellBack: false,
        error: 'timed out waiting for the model backend to resolve',
      }
    }
    await new Promise((resolve) => setTimeout(resolve, 300))
  }
}

/**
 * App is the single Chat screen this task proves end to end: on launch it
 * resolves the embedded server's address via Endpoint(), creates a session,
 * then lets the user send turns and streams the assistant's tokens back as
 * they arrive.
 */
export default function App() {
  const [status, setStatus] = useState<Status>('connecting')
  const [statusDetail, setStatusDetail] = useState('starting embedded server')
  const [backendStatus, setBackendStatus] = useState<BackendStatus | null>(null)
  // fatalError is set only for a startup-time failure (endpoint resolution,
  // session creation, or the LLM backend failing even after the free-provider
  // fallback): it replaces the whole chat screen with a readable error
  // screen. A mid-conversation send error is a different, recoverable thing
  // and stays inline in the transcript instead (see onError in send below).
  const [fatalError, setFatalError] = useState<string | null>(null)
  const [sessionID, setSessionID] = useState<string | null>(null)
  const [lines, setLines] = useState<TranscriptLine[]>([])
  const [input, setInput] = useState('')
  const clientRef = useRef<ApiClient | null>(null)
  const transcriptEndRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      try {
        const endpoint = await getEndpoint()
        const client = new ApiClient(endpoint.baseURL, endpoint.token)
        clientRef.current = client
        const session = await client.createSession()
        if (cancelled) return
        setSessionID(session.id)
        setStatusDetail('waiting for model backend')

        const backend = await waitForBackend(client, () => cancelled)
        if (cancelled) return
        setBackendStatus(backend)
        if (backend.error) {
          setStatus('error')
          setStatusDetail(backend.error)
          setFatalError(backend.error)
          return
        }
        setStatus('ready')
        setStatusDetail(`session ${session.id}`)
      } catch (err) {
        if (cancelled) return
        const message = err instanceof Error ? err.message : String(err)
        setStatus('error')
        setStatusDetail(message)
        setFatalError(message)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    transcriptEndRef.current?.scrollIntoView({ block: 'end' })
  }, [lines])

  async function send() {
    const task = input.trim()
    const client = clientRef.current
    if (!task || !client || !sessionID || status === 'sending') return

    setInput('')
    setLines((prev) => [...prev, { role: 'user', text: task }])
    setLines((prev) => [...prev, { role: 'assistant', text: '' }])
    setStatus('sending')
    setStatusDetail('streaming')

    await client.streamTurn(sessionID, task, {
      onToken: (delta) => {
        setLines((prev) => {
          const next = [...prev]
          const last = next[next.length - 1]
          next[next.length - 1] = { ...last, text: last.text + delta }
          return next
        })
      },
      onDone: () => {
        setStatus('ready')
        setStatusDetail(`session ${sessionID}`)
      },
      onError: (message) => {
        setStatus('error')
        setStatusDetail(message)
        setLines((prev) => [...prev, { role: 'system', text: `error: ${message}` }])
      },
    })
  }

  function onKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      void send()
    }
  }

  return (
    <div className="app">
      <header className="statusbar">
        <span className={`dot dot-${status}`} />
        <span className="statustext">{statusDetail}</span>
      </header>

      {backendStatus && backendStatus.ready && (
        <div className={`backendbar${backendStatus.fellBack ? ' backendbar-fallback' : ''}`}>
          {backendStatus.fellBack
            ? `endpoint ${backendStatus.failedBaseURL} was unreachable; fell back to ${backendStatus.fallbackProfile} (${backendStatus.model})`
            : `using ${backendStatus.model} at ${backendStatus.baseURL}`}
        </div>
      )}

      {fatalError ? (
        <div className="errorscreen">
          <h1>gophermind could not start</h1>
          <pre>{fatalError}</pre>
        </div>
      ) : (
        <>
          <main className="transcript">
            {lines.length === 0 && (
              <div className="hint">
                {status === 'ready' ? 'send a message to start' : 'connecting to the embedded server...'}
              </div>
            )}
            {lines.map((line, i) => (
              <div key={i} className={`line line-${line.role}`}>
                <span className="tag">{line.role}</span>
                <pre className="text">{line.text}</pre>
              </div>
            ))}
            <div ref={transcriptEndRef} />
          </main>

          <footer className="composer">
            <textarea
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={onKeyDown}
              placeholder="type a message, enter to send, shift+enter for a newline"
              disabled={status !== 'ready' && status !== 'sending'}
              rows={3}
            />
            <button
              onClick={() => void send()}
              disabled={status === 'connecting' || status === 'error' || !input.trim()}
            >
              send
            </button>
          </footer>
        </>
      )}
    </div>
  )
}
