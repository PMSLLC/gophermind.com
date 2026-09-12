# Running sessions on another machine

The desktop app can point a session at a `gophermind serve` running elsewhere
instead of its own embedded server. The work, including every shell command and
file edit, then happens on that machine.

## How it fits together

The frontend runs in a WebView and reaches gophermind with `fetch`, which goes
through the OS network stack where no Go dialer can intercept it. So the
desktop runs a small router on loopback and hands the frontend that address:

```
WebView fetch -> 127.0.0.1:PORT (router)
                   |
                   +-- /...            default backend (the embedded server)
                   +-- /b/local/...    the embedded server, explicitly
                   +-- /b/box/...      a remote gophermind serve
```

The frontend holds only the router's token. Each backend is called with its
own, so a remote backend's credential never exists inside the WebView. That
matters because a remote token authorizes shell execution on another machine: a
page-level scripting bug with that token in reach would be remote code
execution rather than local.

## Configuring a backend

`~/.gophermind/backends.json`, which does not exist by default:

```json
[
  {
    "name": "box",
    "kind": "url",
    "base_url": "http://10.0.0.5:8090",
    "token_file": "~/.gophermind/tokens/box"
  }
]
```

The token is named by path and never written in this file. A config file is the
thing most likely to be copied, diffed, pasted into a bug report or swept into a
backup. The file must not be readable by other users:

```sh
mkdir -p ~/.gophermind/tokens
# write the server's GOPHERMIND_SERVE_TOKEN into it, then:
chmod 600 ~/.gophermind/tokens/box
```

`name` becomes a path segment and cannot contain a slash. `local` is reserved
for the embedded server.

One bad entry does not stop the app. A backend whose token file is missing or
world-readable is listed as unavailable with the reason, and the app still
launches with everything else working.

## What changes when a session is remote

**The files being edited are the server's, not yours.** `gophermind serve` runs
with one `--root`, so one server serves one repository. Several repositories
mean several server processes, each its own backend entry.

**Approvals protect that machine, not this one.** The backend name is shown in
the approval bar and beside every approval line for exactly this reason.

**A session belongs to the server holding it.** Switching backends starts a new
session rather than carrying the id across; the sessions list resumes an
existing one on its own backend.

## Server side

Nothing special. A `gophermind serve` with a token set:

```sh
GOPHERMIND_SERVE_TOKEN=... gophermind --root /path/to/repo serve
```

Bind it where only trusted networks can reach it. The bearer token is the only
thing between the network and command execution, and there is no rate limit in
front of it.
