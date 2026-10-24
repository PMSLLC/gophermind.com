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

## Briefs from the browser

With `GOPHERMIND_BRIEFS_DIR` set, a signed-in browser can upload a brief and watch it run: `/app/briefs/new` (upload), `/app/briefs` (runs) and `/app/briefs/<id>` (decomposition, attempts, clarify answers, acceptance, log tail, Stop). Routes: `POST /app/briefs`, `POST /app/briefs/{id}/stop`, `GET /app/briefs/{id}/state`, `GET /app/api/briefs`. Design: `docs/design/web-ui.md`.

| Variable | Meaning |
|---|---|
| `GOPHERMIND_BRIEFS_DIR` | server-owned directory; one folder per run holds `brief.md` (the upload), `run.json` (phase, exit code) and `run.log` (output of both steps). Unset turns the routes off. |
| `GOPHERMIND_BRIEF_CMD` | words put before every `gophermind` call, no shell. Unset runs `gophermind` from `PATH` as the server user. |
| `GOPHERMIND_BRIEF_KILL_CMD` | words put before `-TERM -- -<pgid>` (or `-KILL`) to stop a run's process group. Unset signals the group directly. |

Behaviour:

- The upload is the raw body or a multipart `brief` file, at most 256 KiB, checked with the same parser as `gophermind brief validate`. Its `repo:` must be the absolute path of the server's root (`GOPHERMIND_ROOT`), because the run folder is made inside the brief's repo and the pages read only `<root>/.gophermind/<id>`. Anything else is a 400.
- Two steps run in order, `gophermind brief plan <brief.md> --yes` then `gophermind brief run <id> --repo <root>`, each in its own process group with stdin closed and the server's own secrets (`GOPHERMIND_TOKEN`, session secret, OAuth secret, password hash, HMAC secret) removed from the environment. Output goes to `run.log`.
- One run at a time. A second upload while one is live gets 409 with `active_run_id`; so does an id that was used before. A server restart marks a run that was live as `interrupted`.
- Stop signals the whole process group (SIGTERM, then SIGKILL after 10 seconds) and does not start `brief run` if the plan was still going.

Running as `gmrun` (documentation only, nothing here is applied). The server runs as `gophermind`, and the repo and run folders belong to `gmrun`. Point the launcher at a narrow sudoers rule:

```
# /etc/sudoers.d/gophermind-briefs  (visudo -f)
gophermind ALL=(gmrun) NOPASSWD: /usr/local/bin/gophermind brief plan *, /usr/local/bin/gophermind brief run *
gophermind ALL=(gmrun) NOPASSWD: /bin/kill -TERM -- *, /bin/kill -KILL -- *
```

```
GOPHERMIND_BRIEFS_DIR=/var/lib/gophermind/briefs
GOPHERMIND_BRIEF_CMD=sudo -n -u gmrun /usr/local/bin/gophermind
GOPHERMIND_BRIEF_KILL_CMD=sudo -n -u gmrun /bin/kill
```

`GOPHERMIND_BRIEFS_DIR` must be created by the service (a `StateDirectory=` entry, like the one above, works) and be readable by `gmrun` (mode 0755; the brief and log files are 0644, and a brief holds secret names only, never values). The vault passphrase and provider settings the run needs must be available to `gmrun` itself, not passed through the server. The sudoers wildcards let `gophermind` pass any arguments to those two commands, so review them before use. Not verified on a host: whether the process group of a command started through `sudo` is the group that `kill -- -<pgid>` reaches; check it with a Stop on the first real run.

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
