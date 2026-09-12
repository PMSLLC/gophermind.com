import { useEffect, useRef, useState } from 'react'
import {
  ApiClient,
  type BackendInfo,
  type SessionListItem,
  type BackendStatus,
  type ModelSwitched,
  type PendingApproval,
} from './api/client'
import { waitForEndpoint } from './api/wails'
import ModelPicker from './components/ModelPicker'
import SettingsPanel from './components/SettingsPanel'

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
  /**
   * backend is the machine this tool call would run on. It is recorded on
   * the line rather than read from current state at render time, because the
   * transcript outlives the selection: scrolling back to an approval from an
   * earlier session must show the machine it actually ran on, not whichever
   * one happens to be selected now.
   */
  backend: string
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
 * isEditableTarget reports whether a keyboard event landed in something the
 * user is typing into: an input, a textarea, a select, or any contentEditable
 * element. The approval shortcut below checks it so a word containing "y" or
 * "n" typed into a Settings field cannot silently approve or deny a gated
 * tool call, since preventDefault() would also swallow the keystroke.
 */
function isEditableTarget(target: EventTarget | null): boolean {
  const el = target as HTMLElement | null
  if (!el || typeof el.tagName !== 'string') return false
  if (el.isContentEditable) return true
  const tag = el.tagName.toLowerCase()
  return tag === 'input' || tag === 'textarea' || tag === 'select'
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
  // currentProfile/currentModel are the shared state the model picker and
  // settings panel both need: which model is actually serving this
  // session right now. They start from backendStatus once it resolves and
  // move whenever a model-switched frame arrives mid-turn.
  const [currentProfile, setCurrentProfile] = useState('')
  const [currentModel, setCurrentModel] = useState('')
  const [switchNotice, setSwitchNotice] = useState<ModelSwitched | null>(null)
  const [settingsOpen, setSettingsOpen] = useState(false)
  // backends is every server this desktop can reach; activeBackend is the one
  // the current session runs on. The name is rendered wherever a command can
  // be approved, because "which machine is this about to run on" is the one
  // question the user must never have to guess.
  const [backends, setBackends] = useState<BackendInfo[]>([])
  const [activeBackend, setActiveBackend] = useState('local')
  // Sessions across every reachable backend, each tagged with the machine it
  // lives on. A session belongs to the server holding it, so the backend is
  // part of its identity rather than a display detail.
  const [sessions, setSessions] = useState<{ backend: string; item: SessionListItem }[]>([])
  const [sessionsOpen, setSessionsOpen] = useState(false)
  const clientRef = useRef<ApiClient | null>(null)
  // The endpoint is kept so a client can be rebuilt for another backend.
  // Base URL and token are the router's and do not change with the backend;
  // only the path prefix does.
  const endpointRef = useRef<{ baseURL: string; token: string } | null>(null)
  const transcriptEndRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      try {
        const endpoint = await waitForEndpoint()
        // Diagnostic: name the endpoint in the status line so a failure below
        // says WHICH url could not be reached, not just "Load failed".
        setStatusDetail(`endpoint ${endpoint.baseURL} (token ${endpoint.token ? 'present' : 'MISSING'})`)
        endpointRef.current = { baseURL: endpoint.baseURL, token: endpoint.token }
        const client = new ApiClient(endpoint.baseURL, endpoint.token)
        clientRef.current = client
        // Best effort: an older router without /backends still works, it
        // simply offers only the default backend.
        try {
          const list = await client.listBackends()
          setBackends(list)
          const def = list.find((b) => b.default)
          if (def) setActiveBackend(def.name)
        } catch {
          setBackends([])
        }
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
        setCurrentModel(backend.model)
        setCurrentProfile(backend.fellBack ? backend.fallbackProfile || '' : '')
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
  // decision is actually pending, ignored with a modifier held so it does not
  // collide with OS/browser shortcuts, and ignored while the keystroke is
  // going into an editable element (see isEditableTarget) so typing never
  // decides an approval on the user's behalf.
  useEffect(() => {
    if (!pendingApproval) return
    function onKey(e: KeyboardEvent) {
      if (e.ctrlKey || e.metaKey || e.altKey) return
      if (isEditableTarget(e.target)) return
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
        setLines((prev) => [
          ...prev,
          { kind: 'approval', resolution: 'pending', backend: activeBackend, ...approval },
        ])
        setPendingApproval(approval)
        setStatus('awaiting-approval')
        setStatusDetail(`waiting for your decision on ${approval.tool} (Y to approve, N to deny)`)
      },
      onModelSwitched: (switched) => {
        setCurrentProfile(switched.profile)
        setCurrentModel(switched.model)
        setSwitchNotice(switched)
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

  /**
   * loadSessions asks every available backend for its sessions and merges the
   * answers, tagging each with the machine it came from.
   *
   * One backend being unreachable must not empty the list: a remote box that
   * is down should cost you its own sessions, not the local ones sitting next
   * to them, so each failure is swallowed per backend rather than failing the
   * whole fan-out.
   */
  async function loadSessions() {
    const ep = endpointRef.current
    if (!ep) return
    const usable = backends.length > 0 ? backends.filter((b) => b.available) : [{ name: 'local' } as BackendInfo]
    const results = await Promise.all(
      usable.map(async (b) => {
        const c = new ApiClient(ep.baseURL, ep.token, b.name === 'local' ? '' : b.name)
        try {
          return (await c.listSessions()).map((item) => ({ backend: b.name, item }))
        } catch {
          return []
        }
      }),
    )
    const merged = results.flat()
    merged.sort((a, b) => (a.item.ModTime < b.item.ModTime ? 1 : -1))
    setSessions(merged)
  }

  /**
   * pinModel records the chosen model on the current session, so the next
   * turn uses it instead of the automatic policy.
   *
   * The profile goes with it: a model id is only meaningful at its own
   * provider's endpoint, so pinning one without saying whose it is would
   * send that id wherever the session already points.
   */
  async function pinModel(profile: string, model: string) {
    const client = clientRef.current
    if (!client || !sessionID) return
    try {
      await client.pinSessionModel(sessionID, profile, model)
      setCurrentProfile(profile)
      setCurrentModel(model)
      setLines((prev) => [
        ...prev,
        {
          kind: 'text',
          role: 'system',
          text: `--- model pinned to ${model}${profile ? ` (${profile})` : ''} ---`,
        },
      ])
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err)
      setLines((prev) => [
        ...prev,
        { kind: 'text', role: 'system', text: `could not pin model: ${message}` },
      ])
    }
  }

  /**
   * renameSession gives a session a display name on the backend that holds
   * it. Clearing the name reverts the list to the derived title.
   */
  async function renameSession(backend: string, id: string, current: string) {
    const ep = endpointRef.current
    if (!ep) return
    const next = window.prompt(`Name for this session on ${backend}:`, current)
    if (next === null) return
    const c = new ApiClient(ep.baseURL, ep.token, backend === 'local' ? '' : backend)
    try {
      await c.renameSession(id, next.trim())
      setSessions((prev) =>
        prev.map((s) =>
          s.backend === backend && s.item.ID === id
            ? { ...s, item: { ...s.item, Name: next.trim() } }
            : s,
        ),
      )
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err)
      setLines((prev) => [
        ...prev,
        { kind: 'text', role: 'system', text: `could not rename ${id}: ${message}` },
      ])
    }
  }

  /**
   * removeSession deletes one session from the backend that holds it.
   *
   * Deleting is not undoable, so it asks first and names what is going: the
   * id and the machine, because the same title can exist on two backends and
   * they are different conversations.
   */
  async function removeSession(backend: string, id: string) {
    const ep = endpointRef.current
    if (!ep) return
    if (!window.confirm(`Delete session ${id} on ${backend}? This cannot be undone.`)) return
    const c = new ApiClient(ep.baseURL, ep.token, backend === 'local' ? '' : backend)
    try {
      await c.deleteSession(id)
      setSessions((prev) => prev.filter((s) => !(s.backend === backend && s.item.ID === id)))
      if (id === sessionID) {
        setSessionID(null)
        setStatusDetail('session deleted; send a message to start a new one')
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err)
      setLines((prev) => [
        ...prev,
        { kind: 'text', role: 'system', text: `could not delete ${id}: ${message}` },
      ])
    }
  }

  /** resume attaches the window to an existing session on its own backend. */
  async function resume(backend: string, id: string) {
    const ep = endpointRef.current
    if (!ep) return
    const client = new ApiClient(ep.baseURL, ep.token, backend === 'local' ? '' : backend)
    clientRef.current = client
    setActiveBackend(backend)
    setSessionID(id)
    setSessionsOpen(false)
    setLines([{ kind: 'text', role: 'system', text: `--- resumed ${id} on ${backend} ---` }])
    setStatus('ready')
    setStatusDetail(`session ${id} on ${backend}`)
  }

  /**
   * switchBackend moves the window to another machine.
   *
   * A session belongs to the server that holds it, so this always starts a
   * new one rather than carrying the id across: sending a turn for session X
   * to a machine that has never heard of X would 404, and silently creating
   * it there would split one conversation across two boxes.
   *
   * The transcript is kept and a marker appended, so the record of what ran
   * where survives the switch.
   */
  async function switchBackend(name: string) {
    if (name === activeBackend) return
    const ep = endpointRef.current
    if (!ep) return
    setActiveBackend(name)
    setSessionID(null)
    setStatus('connecting')
    setStatusDetail(`starting a session on ${name}`)
    const client = new ApiClient(ep.baseURL, ep.token, name === 'local' ? '' : name)
    clientRef.current = client
    try {
      const session = await client.createSession()
      setSessionID(session.id)
      setLines((prev) => [
        ...prev,
        { kind: 'text', role: 'system', text: `--- switched to ${name}, new session ${session.id} ---` },
      ])
      setStatus('ready')
      setStatusDetail(`session ${session.id} on ${name}`)
      const st = await client.getBackendStatus()
      setBackendStatus(st)
      setCurrentModel(st.model)
      setCurrentProfile(st.fellBack ? st.fallbackProfile || '' : '')
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err)
      setStatus('error')
      setStatusDetail(`could not start a session on ${name}: ${message}`)
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
        <button
          className="sessionsbtn"
          onClick={() => {
            const next = !sessionsOpen
            setSessionsOpen(next)
            if (next) void loadSessions()
          }}
        >
          sessions
        </button>
        {backends.length > 1 && (
          // The machine this session runs on, always visible. Tool calls
          // execute wherever this points, so it belongs in the chrome rather
          // than behind a settings panel.
          <label className="backendpicker" title="which machine this session runs on">
            <span className="backendpicker-label">on</span>
            <select
              value={activeBackend}
              disabled={status === 'sending' || status === 'awaiting-approval'}
              onChange={(e) => void switchBackend(e.target.value)}
            >
              {backends.map((b) => (
                <option key={b.name} value={b.name} disabled={!b.available}>
                  {b.name}
                  {b.available ? '' : ' (unavailable)'}
                </option>
              ))}
            </select>
          </label>
        )}
        {clientRef.current && (
          <div className="statusbar-models">
            <ModelPicker client={clientRef.current} currentProfile={currentProfile} currentModel={currentModel} onSelect={pinModel}
              />
            <button className="settings-toggle" onClick={() => setSettingsOpen(true)}>
              settings
            </button>
          </div>
        )}
      </header>

      {backendStatus && backendStatus.ready && (
        <div className={`backendbar${backendStatus.fellBack ? ' backendbar-fallback' : ''}`}>
          {backendStatus.fellBack
            ? `endpoint ${backendStatus.failedBaseURL} was unreachable; fell back to ${backendStatus.fallbackProfile} (${backendStatus.model})`
            : `using ${backendStatus.model} at ${backendStatus.baseURL}`}
        </div>
      )}

      {switchNotice && (
        <div className="switchbar" onClick={() => setSwitchNotice(null)}>
          switched to {switchNotice.model} ({switchNotice.profile}): {switchNotice.reason}
        </div>
      )}

      {settingsOpen && clientRef.current && (
        <div className="settings-overlay" onClick={() => setSettingsOpen(false)}>
          <div onClick={(e) => e.stopPropagation()}>
            <SettingsPanel client={clientRef.current} onClose={() => setSettingsOpen(false)} />
          </div>
        </div>
      )}

      {pendingApproval && (
        // Repeats the decision here, outside the scrolling transcript, so
        // it stays visible even if the in-transcript prompt has scrolled
        // out of view - the turn is blocked and that must never look like
        // the app just froze.
        <div className="approvalbar">
          approval needed on <strong>{activeBackend}</strong>:{' '}
          <strong>{pendingApproval.tool}</strong> - press Y to approve, N to deny
        </div>
      )}

      {sessionsOpen && (
        <div className="sessionlist">
          {sessions.length === 0 && <div className="hint">no sessions found</div>}
          {sessions.map(({ backend, item }) => (
            // A row, not a button, because it holds two actions. Nesting the
            // delete button inside a clickable row would be invalid HTML and
            // would resume the session when you meant to remove it.
            <div key={`${backend}:${item.ID}`} className="sessionrow">
              <button className="sessionrow-open" onClick={() => void resume(backend, item.ID)}>
                <span className="sessionrow-backend">{backend}</span>
                <span className="sessionrow-title">{item.Name || item.Title || item.ID}</span>
                <span className="sessionrow-meta">{item.Messages} msgs</span>
              </button>
              <button
                className="sessionrow-rename"
                title={`rename ${item.ID} on ${backend}`}
                onClick={() => void renameSession(backend, item.ID, item.Name || '')}
              >
                rename
              </button>
              <button
                className="sessionrow-delete"
                title={`delete ${item.ID} on ${backend}`}
                onClick={() => void removeSession(backend, item.ID)}
              >
                delete
              </button>
            </div>
          ))}
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
                      <div className="approval-tool">
                        {line.tool}
                        <span className="approval-where"> on {line.backend}</span>
                      </div>
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
