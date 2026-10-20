# Deploying gophermind-server

## Target: the Fedora mini (baby-jesus), jbrahy@<server-host> (aarch64)

The server runs on the Mac mini, which now runs Fedora Asahi Remix (aarch64) under
systemd, not launchd and not the old Ubuntu x86_64 box. `gophermind.service` in this
directory is the unit. Reach the host with `ssh home` and confirm the paths below on
the host before relying on them. Nothing in this repo deploys: John pulls and
restarts. After any deploy, restart the service (`sudo systemctl restart gophermind`)
so it runs the new code.

### Layout

```
/opt/gophermind/
├── gophermind-server    # the binary
├── .env                 # GOPHERMIND_TOKEN, GOPHERMIND_PORT, GOPHERMIND_ROOT
└── workspace/           # GOPHERMIND_ROOT (file/shell tool working dir)

/etc/systemd/system/gophermind.service          # unit file (see gophermind.service)
/etc/systemd/system/gophermind.service.d/
└── 20-llama-config.conf                        # LLM endpoint + model
```

### Build & deploy

Deploy with git, never by copying a file. On the host, fetch and check out the ref,
build there, and restart. The repo has no GitHub key on that host, so fetch over an
agent-forwarded session (`ssh -A`).

```bash
ssh -A home
cd <checkout>          # the repo checkout on the host; confirm its path first
git fetch origin
git checkout <ref>
cd gophermind-server && go build -ldflags="-s -w" -o <binary-path> .
sudo systemctl restart gophermind
git -C <checkout> rev-parse HEAD   # confirm the commit the host runs
```

The install layout and env paths in the rest of this README are unverified for the
mini. Check them over ssh before use.

### .env

```
GOPHERMIND_TOKEN=<64-hex-char bearer token>
GOPHERMIND_PORT=8090
GOPHERMIND_ROOT=/opt/gophermind/workspace
```

### Drop-in: 20-llama-config.conf

```ini
[Service]
Environment="GOPHERMIND_LLM_ENDPOINT=http://127.0.0.1:8083"
Environment="GOPHERMIND_MODEL=qwen3.6-35b-a3b"
```

### Verify

```bash
ssh jbrahy@<server-host> 'curl -s http://127.0.0.1:8090/healthz'
# → ok

ssh jbrahy@<server-host> 'curl -s http://127.0.0.1:8090/readyz'
# → ready
```

### Desktop app connection

The macOS app (`gophermind-osx`) connects to this server in "remote" mode:
- Server URL: `http://<server-host>:8090`
- Token: the `GOPHERMIND_TOKEN` value from `/opt/gophermind/.env`

Or in "local" mode, the app spawns its own `gophermind-server` subprocess
(see `gophermind-osx/main.go` → `findServerBinary`).
