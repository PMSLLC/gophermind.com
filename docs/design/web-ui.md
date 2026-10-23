# Web interface with Google sign-in

Goal (John, 2026-10-23): `https://gophermind.brahy.com/` is a web interface that only a signed-in `@brahy.com` Google account can use. Scope chosen: chat plus runs. Sign-in is enforced in `gophermind-server`, not in a proxy.

## Auth

- Google OAuth 2.0 authorization-code flow, server side. Routes: `GET /auth/login`, `GET /auth/callback`, `POST /auth/logout`.
- `login` sets a short-lived signed `state` cookie (random value, HMAC, 10 minutes, `HttpOnly`, `Secure`, `SameSite=Lax`) and redirects to Google with `hd=<domain>` (a hint only), `scope=openid email`, `state`.
- `callback` checks `state` against the cookie, exchanges the code at Google's token endpoint with the client secret over TLS, and reads the claims from the returned ID token. Because the token comes straight from Google's token endpoint over TLS, its signature need not be verified (OIDC Core 3.1.3.7). It must check `iss` (accounts.google.com), `aud` (the client id), `exp`, `email_verified == true`, and that the `email` ends in `@<domain>` (case-insensitive, exactly one `@`). The `hd` claim is not trusted on its own.
- A pass issues a session cookie `gm_session`: payload `{email, exp}` signed with HMAC-SHA256 under `GOPHERMIND_SESSION_SECRET`, `HttpOnly`, `Secure`, `SameSite=Lax`, 12 hours. Nothing else is stored server side.
- Fail closed: if the OAuth client id, secret or session secret is missing, the server does not start with auth enabled and does not silently serve unauthenticated pages (see Config).

## Request path

A middleware wraps the whole mux. Public paths: `/healthz`, `/readyz`, `/auth/*`, `/run` (webhook, keeps its own bearer+HMAC) and static login assets. Every other path needs a valid `gm_session`. Without one, a browser GET (Accept text/html) is redirected to `/auth/login`; anything else gets 401.

The existing API handlers already require the server bearer token. For a request with a valid session the middleware sets `Authorization: Bearer <server token>` itself (and signs the body when an HMAC secret is configured), so the handlers run unchanged. A client-supplied `Authorization` header on a session-less request still works exactly as before (the native clients). Server token never reaches the browser.

## Pages

- `/` : signed-in home with links to Chat and Runs; replaces the landing page for signed-in users, and signed-out users are sent to sign in.
- `/app/chat` : chat against `POST /session`, `POST /session/{id}/stream` (SSE), `GET /session`.
- `/pipeline` : existing runs dashboard, now behind sign-in and without the token prompt.
- Starting a brief from the browser is a later task: it needs a safe way to launch `gophermind brief run` as the unprivileged `gmrun` user. Out of scope until designed.

## Config

| Variable | Meaning |
|---|---|
| `GOPHERMIND_GOOGLE_CLIENT_ID` / `GOPHERMIND_GOOGLE_CLIENT_SECRET` | OAuth web client (Google Cloud Console) |
| `GOPHERMIND_ALLOWED_DOMAIN` | default `brahy.com` |
| `GOPHERMIND_SESSION_SECRET` | at least 32 bytes, signs cookies |
| `GOPHERMIND_PUBLIC_URL` | `https://gophermind.brahy.com`, builds the redirect URI `.../auth/callback` |

All four of client id, client secret, session secret and public URL set: auth is on. None set: the server behaves as today (bearer API only). Some set: refuse to start.

## Tasks

1. Auth package in `gophermind-lib/serve` (login, callback, logout, session cookie, middleware, bearer bridge), Google endpoints injectable for tests.
2. Wire it into `NewMux`/`gophermind-server` config; deploy docs.
3. Chat page and home page (static, no build step).
4. Server deploy and Google client setup (John creates the OAuth client; secrets only in `/opt/gophermind/.env` on the server).
5. Later: start a brief from the browser.
