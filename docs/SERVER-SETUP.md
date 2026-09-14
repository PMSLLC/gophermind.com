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
