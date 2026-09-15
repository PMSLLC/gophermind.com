# Deploying gophermind-server

## Target: jbrahy@<server-host> (Ubuntu 24.04, x86_64)

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

```bash
# Cross-compile
cd gophermind-server
GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /tmp/gophermind-server .

# Upload
scp /tmp/gophermind-server jbrahy@<server-host>:/tmp/
ssh jbrahy@<server-host> 'sudo cp /tmp/gophermind-server /opt/gophermind/ && sudo chmod 755 /opt/gophermind/gophermind-server && sudo systemctl restart gophermind'
```

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
