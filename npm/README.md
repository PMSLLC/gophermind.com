# gophermind (npm)

A tiny, hackable AI coding agent for your terminal — pointed at your own
OpenAI-compatible LLM.

```sh
npm install -g gophermind
gophermind            # first run walks you through setup, then chats
```

This package downloads the prebuilt `gophermind` binary for your platform
(macOS / Linux / Windows, x64 / arm64) from the project's GitHub Releases on
install, and exposes it as the `gophermind` command.

- Full docs & source: **https://github.com/jbrahy/gophermind.com**
- Prefer Homebrew on macOS? `brew install jbrahy/tap/gophermind`

Environment knobs for install:

- `GOPHERMIND_SKIP_DOWNLOAD=1` — skip the binary download (e.g. CI that doesn't run it)
- `GOPHERMIND_DOWNLOAD_BASE=<url>` — override the release download base URL

MIT licensed. Startup fortunes © Brian M. Clapper, CC BY 4.0.
