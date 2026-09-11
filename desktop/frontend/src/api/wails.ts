// wails.ts is the ONLY file in this frontend that talks to Wails directly. It
// wraps the single bound Go method, Endpoint, which returns the embedded
// server's base URL and bearer token. Everything else in this app is plain
// fetch/SSE against that address (see client.ts) - the central design
// decision from the desktop app spec: embedded and remote modes share one
// code path, and Wails never grows a binding per operation.

/** EndpointInfo is what Endpoint() resolves to. */
export interface EndpointInfo {
  baseURL: string
  token: string
}

declare global {
  interface Window {
    go?: {
      main?: {
        App?: {
          Endpoint?: () => Promise<EndpointInfo>
        }
      }
    }
  }
}

/**
 * getEndpoint calls the bound Go App.Endpoint method and returns the
 * embedded server's base URL and bearer token. Throws if the Wails runtime
 * binding is not present (for example, running the frontend outside the
 * Wails shell).
 */
export async function getEndpoint(): Promise<EndpointInfo> {
  const endpoint = window.go?.main?.App?.Endpoint
  if (!endpoint) {
    throw new Error('wails binding window.go.main.App.Endpoint is not available')
  }
  return endpoint()
}

/**
 * waitForEndpoint polls getEndpoint until the embedded server has started.
 *
 * The frontend mounts before the Go side finishes binding its listener, so the
 * first call reliably rejects with "embedded server is still starting". That
 * is expected, not an error worth showing: this retries until the server is up
 * or the budget runs out, and only then reports a failure.
 */
export async function waitForEndpoint(
  timeoutMs = 30000,
  intervalMs = 250,
): Promise<EndpointInfo> {
  const deadline = Date.now() + timeoutMs
  let lastErr: unknown
  for (;;) {
    try {
      const ep = await getEndpoint()
      if (ep.baseURL) return ep
      lastErr = new Error('embedded server reported no base URL')
    } catch (err) {
      lastErr = err
    }
    if (Date.now() >= deadline) {
      const detail = lastErr instanceof Error ? lastErr.message : String(lastErr)
      throw new Error(`embedded server did not start within ${timeoutMs / 1000}s: ${detail}`)
    }
    await new Promise((r) => setTimeout(r, intervalMs))
  }
}
