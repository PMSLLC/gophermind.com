import { useEffect, useRef, useState } from 'react'
import { ApiClient } from './api/client'
import { getEndpoint } from './api/wails'

interface TranscriptLine {
  role: 'user' | 'assistant' | 'system'
  text: string
}

type Status = 'connecting' | 'ready' | 'sending' | 'error'

/**
 * App is the single Chat screen this task proves end to end: on launch it
 * resolves the embedded server's address via Endpoint(), creates a session,
 * then lets the user send turns and streams the assistant's tokens back as
 * they arrive.
 */
export default function App() {
  const [status, setStatus] = useState<Status>('connecting')
  const [statusDetail, setStatusDetail] = useState('starting embedded server')
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
        setStatus('ready')
        setStatusDetail(`session ${session.id}`)
      } catch (err) {
        if (cancelled) return
        setStatus('error')
        setStatusDetail(err instanceof Error ? err.message : String(err))
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
        <button onClick={() => void send()} disabled={status === 'connecting' || status === 'error' || !input.trim()}>
          send
        </button>
      </footer>
    </div>
  )
}
