# Changelog

All notable changes to GopherMind are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/), and the project aims to follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]
### Added
- `gophermind --print` non-interactive mode that speaks a Claude-Code-compatible **stream-json** protocol (init / assistant / tool_use / tool_result / result messages), so external drivers such as OpenCoven's Coven can run gophermind programmatically. Supports text and stream-json input/output.
- **Session persistence**: `--session-id` pre-assigns a session and `--resume` continues a saved one, so a driver can keep state across processes. Histories are stored per-id (path-traversal-guarded) as JSONL.
- Print-mode `--append-system-prompt` and `--permission-mode` (`auto` full access / `plan` read-only, which denies edits & shell).
- An **OpenCoven runtime manifest** (`coven/gophermind.json`, schema-validated) declaring GopherMind as a streaming Coven runtime (stream + pre-assigned sessions + sandbox mapping).
- **npm distribution** (`npm install -g gophermind`): cross-platform (macOS/Linux/Windows, x64/arm64) prebuilt binaries via a postinstall downloader; GoReleaser now builds Linux and Windows archives too.
- A `--version` flag (alongside the `version` subcommand).

## [0.1.0] - 2026-07-09
### Added
- First-run setup wizard — endpoint, API key, model picker, approval mode, and max iterations — saved to a global config, plus a `gophermind config` command to re-run it.
- Signed + notarized macOS release pipeline (GoReleaser) distributed via a Homebrew cask, and a `gophermind version` command.
- A random fortune, the version, and recent changes shown under the gopher banner on startup.
- A redrawn gopher ASCII banner.

### Changed
- No endpoint is baked into the binary anymore — configure it via the wizard, a provider profile, or `GOPHERMIND_BASE_URL`.

### Fixed
- The terminal OSC-11 background-color query no longer leaks escape codes into the input box.
