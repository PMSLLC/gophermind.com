# Server Setup for gophermind.com

Configuration for running gophermind on the remote server (`<server-host>`) and enabling autonomous agent execution.

## Server Location

- **Host**: `<server-host>` (AWS EC2, private VPC)
- **SSH User**: `gophermind` (dedicated agent user)
- **SSH Config**: Add to `~/.ssh/config` for convenience:
  ```
  Host gophermind-server
      HostName <server-host>
      User gophermind
  ```
- **Access**: `ssh gophermind-server` or `ssh gophermind@<server-host>`

## Repository on Server

**Location**: `/home/gophermind/workspace/gophermind.com/`

Cloned from: https://github.com/jbrahy/gophermind.com.git

Workspace structure:
```
/home/gophermind/workspace/
├── gophermind.com/         ← This project (cloned from GitHub)
├── BraiNIXOS/              ← Other projects in gophermind workspace
├── CEOd/
├── longbeachpokerclub.com/
└── .gophermind/            ← Shared gophermind config
```

## Gophermind Service

**Service**: `gophermind.service` (systemd)

**Status**:
```bash
ssh gophermind-server "systemctl status gophermind.service"
```

**Configuration**:
```bash
ExecStart=/usr/local/bin/gophermind --root /home/gophermind/workspace serve
```

- Root directory: `/home/gophermind/workspace`
- Port: `8090` (accessible at `http://<server-host>:8090`)
- Model: Local `qwen3.6-35b` (no external API calls)
- Auto-restart on reboot (enabled preset)

**Logs**:
```bash
ssh gophermind-server "journalctl -u gophermind.service -f"
```

**Restart**:
```bash
ssh gophermind-server "sudo systemctl restart gophermind.service"
```

## Registering gophermind.com with the Server

Once the repo is cloned, register it so the gophermind server knows about it:

```bash
# Method 1: Direct HTTP API (if available)
curl -X POST http://<server-host>:8090/projects \
  -H "Content-Type: application/json" \
  -d '{
    "name": "gophermind",
    "path": "/home/gophermind/workspace/gophermind.com",
    "config_url": "https://raw.githubusercontent.com/jbrahy/gophermind.com/main/GOPHERMIND.toml"
  }'

# Method 2: Via gophermind CLI (if command is available)
ssh gophermind-server "gophermind project register \
  --name gophermind \
  --path /home/gophermind/workspace/gophermind.com \
  --config-url https://raw.githubusercontent.com/jbrahy/gophermind.com/main/GOPHERMIND.toml"
```

## Configuration Files Used

The server reads these files to understand the project:

1. **GOPHERMIND.toml** — Project metadata (structure, build commands, conventions)
2. **docs/ARCHITECTURE-INDEX.md** — Architecture reference for semantic search
3. **.gophermind/skills/*.md** — Custom skill packs (injected on every turn)
4. **PROJECT.md** — Project conventions and secrets locations

All are in the cloned repo; no additional server-side configuration needed.

## Google Sign-In (optional)

With these variables set, `gophermind-server` puts every route except `/healthz`, `/readyz`, `/auth/*` and `/run` behind a Google sign-in. A signed-in browser session is given the server bearer token by the server; the token never reaches the browser. Native clients that send their own `Authorization` header are unchanged. Design: `docs/design/web-ui.md`.

| Variable | Meaning |
|---|---|
| `GOPHERMIND_GOOGLE_CLIENT_ID` | OAuth web client id |
| `GOPHERMIND_GOOGLE_CLIENT_SECRET` | OAuth web client secret |
| `GOPHERMIND_SESSION_SECRET` | signs the cookies, at least 32 bytes |
| `GOPHERMIND_PUBLIC_URL` | external base URL, for example `https://gophermind.brahy.com`; the redirect URI is `<url>/auth/callback` |
| `GOPHERMIND_ALLOWED_DOMAIN` | email domain allowed in, default `brahy.com` |

Set the first four to turn it on, or none to leave it off. A partial set makes the server refuse to start and name the missing variables. Keep the values in the server's env file, not in tracked files.

## Writable state directory

The unit runs with `ProtectSystem=strict`, so the service user's home (`/opt/gophermind`) is read-only and web chat sessions fail with `create transcript dir ... read-only file system`. A drop-in gives the service a writable state directory (`/etc/systemd/system/gophermind.service.d/state.conf`):

```
[Service]
StateDirectory=gophermind
Environment=GOPHERMIND_CONFIG_DIR=/var/lib/gophermind
```

Then `sudo systemctl daemon-reload && sudo systemctl restart gophermind`.

## Temporary password login

Until a Google OAuth client exists, a single username and password can sign in to the web interface. It is temporary and is removed once Google sign-in is live (the delete list is in the header of `gophermind-lib/serve/passwordauth.go`). A successful login issues the same `gm_session` cookie as Google sign-in.

Make the hash on any machine (the password comes from stdin, never argv, and is not printed back; only the hash line is printed):

```bash
gophermind-server hashpw
```

Put these in the server's env file, not in tracked files. Never put the plaintext password in the env.

| Variable | Meaning |
|---|---|
| `GOPHERMIND_WEB_USER` | the username |
| `GOPHERMIND_WEB_PASSWORD_HASH` | the `pbkdf2-sha256$...` line from `hashpw` |
| `GOPHERMIND_SESSION_SECRET` | at least 32 bytes (shared with Google sign-in) |
| `GOPHERMIND_PUBLIC_URL` | external base URL (shared with Google sign-in) |

User and hash set with the session secret and public URL: the local login is on, and it can be on alongside Google (`/auth/login` still goes to Google; `/auth/login?local=1` shows the password form). With only the local login, `/auth/login` shows the form. A partial set refuses to start and names the missing variables. After 5 failed attempts from one client address in 15 minutes the server answers 429 until the window ends; the limiter is in memory and restarts clear it.

## Running Tasks

Once registered, gophermind can work on tasks:

```bash
# Desktop or web client connects to http://<server-host>:8090
# Select "gophermind" project
# Type a task: "add a new tool to read JSON files"
# gophermind executes in /home/gophermind/workspace/gophermind.com/
```

## Keeping Server Copy in Sync

The cloned repo should stay up-to-date:

```bash
# From server
ssh gophermind-server "cd /home/gophermind/workspace/gophermind.com && git pull --ff-only"

# Or via cron (if automated pulls are desired)
# 0 * * * * cd /home/gophermind/workspace/gophermind.com && git pull --ff-only
```

## Troubleshooting

**"gophermind service not running"**
```bash
ssh gophermind-server "sudo systemctl status gophermind.service"
ssh gophermind-server "sudo systemctl restart gophermind.service"
```

**"Project not found"** — Re-register:
```bash
curl -X POST http://<server-host>:8090/projects \
  -d '{"name":"gophermind","path":"/home/gophermind/workspace/gophermind.com"}'
```

**"Changes from main branch not visible"** — Pull latest:
```bash
ssh gophermind-server "cd /home/gophermind/workspace/gophermind.com && git pull"
```

**"Agent can't find GOPHERMIND.toml"** — Verify file exists:
```bash
ssh gophermind-server "ls -la /home/gophermind/workspace/gophermind.com/GOPHERMIND.toml"
```

## Related Documentation

- **GOPHERMIND.toml** — Project metadata
- **docs/GOPHERMIND-SETUP.md** — RAG indexing and skills setup
- **docs/ARCHITECTURE-INDEX.md** — Architecture reference
- **docs/DEPLOYMENT.md** — Local serve mode setup
