import { useEffect, useRef, useState } from 'react'
import { ApiClient, type BackendStatus, type PendingApproval } from './api/client'
import { waitForEndpoint } from './api/wails'

/** TextLine is a plain chat line: something the user typed, or prose back. */
interface TextLine {
  kind: 'text'
  role: 'user' | 'assistant' | 'system'
  text: string
}

/**
 * ApprovalResolutionState is where one approval prompt stands: pending
 * (buttons live, keys live), or one of the ways it stopped being pending.
 * already-resolved means the server's 404 told us someone else answered it
 * first - not an error.
 */
type ApprovalResolutionState = 'pending' | 'approved' | 'denied' | 'already-resolved'

/**
 * ApprovalLine renders a gated tool call in the transcript, in place, so the
 * approve/deny decision sits next to the call it is about instead of in a
 * separate panel.
 */
interface ApprovalLine extends PendingApproval {
  kind: 'approval'
  resolution: ApprovalResolutionState
}

type TranscriptLine = TextLine | ApprovalLine

type Status = 'connecting' | 'ready' | 'sending' | 'awaiting-approval' | 'error'

// backendPollTimeoutMs bounds how long the app waits for GET /backend-status
// to report ready or error before giving up and showing a timeout error
// itself. Backend resolution (see desktop/backend.go's resolveLLMBackend)
// is bounded by a short liveness probe on the configured endpoint, plus at
// most one more on the free-provider fallback, so it should finish in single
// digit seconds; this is a generous ceiling, not the expected case.
const backendPollTimeoutMs = 30000

/**
 * prettyArgs pretty-prints a gated tool call's arguments (a JSON string) so
 * a human reviewing it can actually read what is about to run. Falls back
 * to the raw string if it does not parse as JSON, rather than hiding it.
 */
function prettyArgs(argsJSON: string): string {
  try {
    return JSON.stringify(JSON.parse(argsJSON), null, 2)
  } catch {
    return argsJSON
  }
}

/**
 * argsHeadline pulls out the one field (a shell command or a file path)
 * that makes a gated call obvious at a glance, so it does not require
 * reading the full pretty-printed JSON to see what is about to happen.
 * Returns null when neither field is present.
 */
function argsHeadline(argsJSON: string): string | null {
  try {
    const parsed = JSON.parse(argsJSON) as Record<string, unknown>
    if (typeof parsed.command === 'string') return parsed.command
    if (typeof parsed.path === 'string') return parsed.path
  } catch {
    // Not JSON, or not an object: no headline, prettyArgs still shows the
    // raw text below.
  }
  return null
}

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
  // pendingApproval is the one gated tool call currently blocking the turn,
  // if any. It drives both the Y/N keyboard shortcut and the approval bar
  // below, in addition to the in-transcript prompt.
  const [pendingApproval, setPendingApproval] = useState<PendingApproval | null>(null)
  const clientRef = useRef<ApiClient | null>(null)
  const transcriptEndRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      try {
        const endpoint = await waitForEndpoint()
        // Diagnostic: name the endpoint in the status line so a failure below
        // says WHICH url could not be reached, not just "Load failed".
        setStatusDetail(`endpoint ${endpoint.baseURL} (token ${endpoint.token ? 'present' : 'MISSING'})`)
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
        // Include the resolved endpoint so an opaque WebView error ("Load
        // failed") still tells us what it was trying to reach.
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

  // Keyboard-first approval, so deciding is never slower than the TUI's
  // y/N prompt: Y approves, N denies, no Enter required. Bound only while a
  // decision is actually pending, and ignored with a modifier held so it
  // does not collide with OS/browser shortcuts.
  useEffect(() => {
    if (!pendingApproval) return
    function onKey(e: KeyboardEvent) {
      if (e.ctrlKey || e.metaKey || e.altKey) return
      if (e.key === 'y' || e.key === 'Y') {
        e.preventDefault()
        void decideApproval(true)
      } else if (e.key === 'n' || e.key === 'N') {
        e.preventDefault()
        void decideApproval(false)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pendingApproval])

  async function send() {
    const task = input.trim()
    const client = clientRef.current
    if (!task || !client || !sessionID || status === 'sending' || status === 'awaiting-approval') return

    setInput('')
    setLines((prev) => [...prev, { kind: 'text', role: 'user', text: task }])
    setLines((prev) => [...prev, { kind: 'text', role: 'assistant', text: '' }])
    setStatus('sending')
    setStatusDetail('streaming')

    await client.streamTurn(sessionID, task, {
      onToken: (delta) => {
        setLines((prev) => {
          const last = prev[prev.length - 1]
          // A tool call (and its approval prompt) may have landed after the
          // in-progress assistant line, so tokens resuming after a decision
          // start a fresh line instead of appending to a stale one.
          if (last && last.kind === 'text' && last.role === 'assistant') {
            const next = [...prev]
            next[next.length - 1] = { ...last, text: last.text + delta }
            return next
          }
          return [...prev, { kind: 'text', role: 'assistant', text: delta }]
        })
      },
      onApprovalNeeded: (approval) => {
        setLines((prev) => [...prev, { kind: 'approval', resolution: 'pending', ...approval }])
        setPendingApproval(approval)
        setStatus('awaiting-approval')
        setStatusDetail(`waiting for your decision on ${approval.tool} (Y to approve, N to deny)`)
      },
      onDone: () => {
        setStatus('ready')
        setStatusDetail(`session ${sessionID}`)
      },
      onError: (message) => {
        setStatus('error')
        setStatusDetail(message)
        setLines((prev) => [...prev, { kind: 'text', role: 'system', text: `error: ${message}` }])
      },
    })
  }

  /**
   * decideApproval posts the human's decision for the currently pending
   * approval. A 404 (the contract's "already resolved") is rendered as
   * such, not as an error: something else (the desktop gate's own timeout,
   * or a duplicate click) already answered it.
   */
  async function decideApproval(approved: boolean) {
    const client = clientRef.current
    const approval = pendingApproval
    if (!client || !sessionID || !approval) return

    setPendingApproval(null)
    setStatus('sending')
    setStatusDetail('streaming')

    try {
      const result = await client.resolveApproval(sessionID, approval.approvalID, approved)
      setLines((prev) =>
        prev.map((line) =>
          line.kind === 'approval' && line.approvalID === approval.approvalID
            ? {
                ...line,
                resolution: result.alreadyResolved ? 'already-resolved' : approved ? 'approved' : 'denied',
              }
            : line,
        ),
      )
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err)
      setLines((prev) => [
        ...prev,
        { kind: 'text', role: 'system', text: `approval decision failed: ${message}` },
      ])
    }
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

      {pendingApproval && (
        // Repeats the decision here, outside the scrolling transcript, so
        // it stays visible even if the in-transcript prompt has scrolled
        // out of view - the turn is blocked and that must never look like
        // the app just froze.
        <div className="approvalbar">
          approval needed: <strong>{pendingApproval.tool}</strong> - press Y to approve, N to deny
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
            {lines.map((line, i) => {
              if (line.kind === 'approval') {
                const headline = argsHeadline(line.args)
                return (
                  <div key={i} className={`line line-approval approval-${line.resolution}`}>
                    <span className="tag">approval</span>
                    <div className="approval">
                      <div className="approval-tool">{line.tool}</div>
                      {headline && <div className="approval-headline">{headline}</div>}
                      <pre className="approval-args">{prettyArgs(line.args)}</pre>
                      {line.resolution === 'pending' ? (
                        <div className="approval-actions">
                          <button className="approve" onClick={() => void decideApproval(true)}>
                            approve (Y)
                          </button>
                          <button className="deny" onClick={() => void decideApproval(false)}>
                            deny (N)
                          </button>
                        </div>
                      ) : (
                        <div className="approval-outcome">
                          {line.resolution === 'approved' && 'approved'}
                          {line.resolution === 'denied' && 'denied'}
                          {line.resolution === 'already-resolved' && 'already resolved elsewhere'}
                        </div>
                      )}
                    </div>
                  </div>
                )
              }
              return (
                <div key={i} className={`line line-${line.role}`}>
                  <span className="tag">{line.role}</span>
                  <pre className="text">{line.text}</pre>
                </div>
              )
            })}
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
              disabled={
                status === 'connecting' ||
                status === 'error' ||
                status === 'awaiting-approval' ||
                !input.trim()
              }
            >
              send
            </button>
          </footer>
        </>
      )}
    </div>
  )
}
