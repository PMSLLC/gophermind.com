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

With `GOPHERMIND_BRIEFS_DIR` set, a signed-in browser can upload a brief and watch it run: `/app/briefs/new` (upload), `/app/briefs` (runs) and `/app/briefs/<id>` (decomposition, attempts, clarify answers, acceptance, log tail, Stop). Routes: `POST /app/briefs` (add `?ask=1` to be asked the clarify questions), `POST /app/briefs/{id}/stop`, `POST /app/briefs/{id}/answer`, `GET /app/briefs/{id}/state`, `GET /app/briefs/{id}/events` (server-sent events), `GET /app/api/briefs`. Design: `docs/design/web-ui.md`.

| Variable | Meaning |
|---|---|
| `GOPHERMIND_BRIEFS_DIR` | server-owned directory; one folder per run holds `brief.md` (the upload), `run.json` (phase, exit code) and `run.log` (output of both steps). Unset turns the routes off. |
| `GOPHERMIND_BRIEF_CMD` | words put before every `gophermind` call, no shell. Unset runs `gophermind` from `PATH` as the server user. |
| `GOPHERMIND_BRIEF_KILL_CMD` | words put before `-TERM -- -<pgid>` (or `-KILL`) to stop a run's process group. Unset signals the group directly. |

Behaviour:

- The upload is the raw body or a multipart `brief` file, at most 256 KiB, checked with the same parser as `gophermind brief validate`. Its `repo:` must be the absolute path of the server's root (`GOPHERMIND_ROOT`), because the run folder is made inside the brief's repo and the pages read only `<root>/.gophermind/<id>`. Anything else is a 400.
- Two steps run in order, `gophermind brief plan <brief.md> --yes` then `gophermind brief run <id> --repo <root>`, each in its own process group with stdin closed and the server's own secrets (`GOPHERMIND_TOKEN`, session secret, OAuth secret, password hash, HMAC secret) removed from the environment. Output goes to `run.log`.
- One run at a time. A second upload while one is live gets 409 with `active_run_id`; so does an id that was used before. A server restart marks a run that was live as `interrupted`.
- With `?ask=1` on the upload (the Upload page has a checkbox) planning uses the file gate: `gophermind brief plan <brief.md> --yes --gate file`. When the planner has open clarify questions it writes `QUESTIONS.md` in the run folder and exits 3; the run shows as `waiting`, no process runs and the one-run slot is free. The run page lists each question with an answer box and, when the model recommended one, a button for it. `POST /app/briefs/{id}/answer` with `{"question_id", "answer"}` (answer at most 8 KiB, no line of three backticks) fills that question's block in `QUESTIONS.md`; when every block holds text it runs `gophermind brief resume <id> --yes --gate file` and then `brief run`, like a normal upload. Answers: 200 saved with `remaining`, 202 resumed, 400 bad id or answer, 404 unknown run or question, 409 run not waiting or another run active. Approval of the plan stays unattended (`--yes`); only the questions wait for you. Without `ask` nothing changes.
- The run page follows `GET /app/briefs/{id}/events`: a `state` event at once and again whenever the run folder or log changes (the server checks every second with a content hash), then an `end` event once the run is final. At most 8 streams are open at a time (503 beyond that); a stream ends when the browser leaves. If the stream fails the page polls `/state` every 3 seconds as before.
- `/state` also carries `wave_checks` (per wave: leaves, verified count, check `pass`, `fail`, `repairing`, `running` or `pending`, and the failing leaf ids) and `repairs` (the report's count of leaf reopenings and the leaves being repaired now, with attempts so far and the reason). The executor keeps no log of each check, so a wave's result is derived from leaf statuses, `_state/leaf-results.json` and the run's stop reason; a repair that already succeeded is counted only in the total.
- Stop signals the whole process group (SIGTERM, then SIGKILL after 10 seconds) and does not start `brief run` if the plan was still going.

Running as `gmrun` (as deployed on development-jb). The server runs as `gophermind`, and the repo and run folders belong to `gmrun`. The launcher goes through a narrow sudoers rule in `/etc/sudoers.d/gophermind-briefs` (edit with `visudo -f`). `/bin` is a symlink to `/usr/bin` on this host, so the kill binary must be named `/usr/bin/kill`; a rule for `/bin/kill` never matches what sudo resolves.

```
# /etc/sudoers.d/gophermind-briefs
gophermind ALL=(gmrun) NOPASSWD: /usr/local/bin/gophermind brief plan *, /usr/local/bin/gophermind brief resume *, /usr/local/bin/gophermind brief run *
gophermind ALL=(gmrun) NOPASSWD: /usr/bin/kill -TERM -- *, /usr/bin/kill -KILL -- *
```

`brief resume` is needed only for runs uploaded with `ask=1`; add it to a sudoers file written before that feature existed.

```
GOPHERMIND_BRIEFS_DIR=/var/lib/gophermind/briefs
GOPHERMIND_BRIEF_CMD=sudo -n -u gmrun /usr/local/bin/gophermind
GOPHERMIND_BRIEF_KILL_CMD=sudo -n -u gmrun /usr/bin/kill
```

The service needs a systemd drop-in, `/etc/systemd/system/gophermind.service.d/briefs.conf`:

```
[Service]
NoNewPrivileges=false
ProtectHome=false
ReadWritePaths=/home/gmrun
```

Why: `sudo` is a setuid program, and `NoNewPrivileges=true` (the base unit's hardening) stops a setuid binary from gaining privileges, so every launch would fail with "The \"no new privileges\" flag is set". The run's repo and run folders live under `gmrun`'s home, so `ProtectHome=false` lets the service see `/home` and `ReadWritePaths=/home/gmrun` lets it write there (the server reads the run folder for the pages and, for answers, edits `QUESTIONS.md` in it). Then `sudo systemctl daemon-reload && sudo systemctl restart gophermind`.

`GOPHERMIND_BRIEFS_DIR` is `/var/lib/gophermind/briefs`, owned by `gophermind`, mode 0755 so `gmrun` can read it (a `StateDirectory=` entry creates it; the brief and log files are 0644, and a brief holds secret names only, never values). The vault passphrase and provider settings the run needs must be available to `gmrun` itself, not passed through the server. The sudoers wildcards let `gophermind` pass any arguments to those commands, so review them before use. Not verified on a host: whether the process group of a command started through `sudo` is the group that `kill -- -<pgid>` reaches (check it with a Stop on a real run), and whether the server user can read and replace `QUESTIONS.md`: the planner creates the run folder 0700 and the gate writes the file 0600 as `gmrun`, so answering needs the server user to have access to both. If a first `ask=1` run answers with 409 or 500, check that access first.

## Removed: temporary password login

The temporary username and password login is gone; sign-in is Google only. `GOPHERMIND_WEB_USER` and `GOPHERMIND_WEB_PASSWORD_HASH` are ignored and should be removed from the server's env file, and the `gophermind-server hashpw` subcommand no longer exists. `/auth/login?local=1` behaves like `/auth/login`.

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
