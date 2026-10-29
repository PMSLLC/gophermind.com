---
spec_version: "2.0"
id: gm-2026-10-28-001
title: linkcheck
language: go
base_branch: main
landing: commit
on_ambiguity: assume_and_document
network:
  - host: proxy.golang.org
    purpose: Module downloads
    critical: true
  - host: sum.golang.org
    purpose: Module checksum database
    critical: true
---

## Overview

`linkcheck` is a small command-line tool that reads one Markdown or HTML file, extracts every web link in it, and checks whether each link still resolves. It prints one line per link and a summary, and exits non-zero when a link is broken, so it can run in CI on a docs repository. Links are checked concurrently. An optional `--fix` mode rewrites redirected links to their final address. It is one Go binary with no configuration file.

## Features

### Extract links

`Extract` finds link targets in text, in order of appearance, whatever the file extension.

- A Markdown link or image `[text](url)` or `![alt](url)` yields `url`, ended by the first `)` or white space.
- A Markdown autolink `<url>` yields `url` when it starts with `http://` or `https://`.
- An HTML attribute `href="url"` or `src="url"`, with double or single quotes and any letter case in the attribute name, yields `url`.
- Each result records the byte offsets of the URL in the text.
- Empty input and input with no links yield an empty list.

### Check links

`linkcheck docs.md` checks every distinct `http` and `https` URL once, with a GET request, and prints one line per URL in order of first appearance.

- A URL checked more than once in the file is checked and printed once.
- A URL whose scheme is not `http` or `https` (relative paths, `#anchors`, `mailto:`, empty targets) is not checked; it counts as skipped.
- Redirect statuses 301, 302, 303, 307 and 308 are followed, resolving a relative `Location` against the current URL, up to 10 redirects.
- A final status from 200 to 299 prints `ok <url>`, or `redirected <url> -> <final>` when at least one redirect was followed.
- Any other final status prints `broken <url> (HTTP <code>)`, for example `broken http://x/ (HTTP 404)`.
- More than 10 redirects prints `broken <url> (too many redirects)`.
- A request that runs past the timeout prints `broken <url> (timeout)`.
- Any other failure, such as connection refused, prints `broken <url> (request failed)`.
- The last line is `checked <n> links: <a> ok, <b> redirected, <c> broken, <d> skipped`, where `n` is `a+b+c`.
- An empty file, or a file with no links, prints `checked 0 links: 0 ok, 0 redirected, 0 broken, 0 skipped` and exits 0.
- The exit code is 0 when no link is broken and 1 when at least one is.

### Concurrency and timeout

- `--workers N` sets how many requests run at once; the default is 8 and N must be 1 or more.
- `--timeout D` sets the limit for one URL including its redirects, in `time.ParseDuration` syntax; the default is `10s` and D must be positive.
- Output order never depends on which request finishes first.

### Fix redirects

`linkcheck --fix docs.md` does a normal check, then rewrites the file.

- Every link target whose result is `redirected` is replaced by its final URL; broken and ok links are left as they are.
- The file is written only when at least one target changed, and a line `fixed: <n>` is printed before the summary, where `n` is the number of distinct URLs replaced.
- The exit code is still decided by broken links, not by fixes.

### Report errors

- A file that cannot be opened prints `linkcheck: <error>` on standard error and exits with code 2.
- No file argument, or more than one, prints `usage: linkcheck [--fix] [--workers N] [--timeout D] FILE` on standard error and exits with code 2.
- An invalid `--workers` or `--timeout` value prints the same usage line and exits with code 2.
- The file name `-` reads standard input; with `--fix` it prints `linkcheck: --fix needs a file, not standard input` on standard error and exits with code 2.

## Architecture

- `cmd/linkcheck` (package `main`): parses flags, opens the file or chooses `os.Stdin`, calls the packages below, prints the lines, writes the fixed file and decides the exit code. Nothing else.
- `internal/links`: `Extract(r io.Reader) ([]Link, error)` and `Rewrite(src string, repl map[string]string) string`. No network, no files.
- `internal/check`: `All(ctx context.Context, urls []string, client *http.Client, workers int) []Result` checks the URLs with a bounded worker pool and returns results in input order. It does not print.
- `internal/report`: `Write(w io.Writer, results []Result, skipped int, fixed int, showFixed bool)` formats the lines and the summary.

Data flow: `main` reads the input, `links.Extract` lists targets, `main` drops duplicates and splits off the skipped ones, `check.All` resolves the rest, `report.Write` prints, and `links.Rewrite` produces the fixed text.

Patterns to follow:

- Functions that read input take an `io.Reader`; functions that print take an `io.Writer`. Only `main` chooses `os.Stdin`, `os.Stdout` and `os.Stderr`.
- The client passed to `check.All` does not follow redirects itself (`CheckRedirect` returns `http.ErrUseLastResponse`); `check` follows them so it can count hops and report the final URL.
- Errors are wrapped with `fmt.Errorf("...: %w", err)` and only `main` decides the exit code.

## Data

- `Link` holds `URL string`, `Start int` and `End int` (byte offsets of the URL in the text).
- `Result` holds `URL string`, `Status string` (one of `ok`, `redirected`, `broken`), `Final string` (the last URL reached) and `Reason string` (the text inside the parentheses for a broken link, otherwise empty).

## Constraints

- Standard library only.
- Go 1.22 or later.
- `gofmt` clean and `go vet` clean.
- Tests are table driven and use `strings.NewReader` and `bytes.Buffer` for input and output.
- Tests that check links use `net/http/httptest` servers, never the real network and never a real file.
- Timeout tests use a short `--timeout` or client timeout against a handler that waits, so they finish in well under a second.
- A test shows that no more than `workers` requests are in flight at once and that more than one runs at once when `workers` is 2 or more.

## Out of scope

- Checking relative links against the file system, or `#anchor` targets.
- Crawling the pages that links point to.
- HEAD requests, retries, caching, or authentication.
- Reading several files, writing a report file, or any output format other than the lines above.
- Full Markdown or HTML parsing.

## Acceptance

- `go build ./...` succeeds.
- `go vet ./...` reports nothing.
- `go test ./...` passes.
- `printf '' | linkcheck -` prints `checked 0 links: 0 ok, 0 redirected, 0 broken, 0 skipped` and exits with code 0.
- `printf '[a](#top) [b](page.html) [c](mailto:x@y.z)\n' | linkcheck -` prints `checked 0 links: 0 ok, 0 redirected, 0 broken, 3 skipped` and exits with code 0.
- `printf '[x](http://127.0.0.1:1/) [y](http://127.0.0.1:1/)\n' | linkcheck -` prints `broken http://127.0.0.1:1/ (request failed)` once, then `checked 1 links: 0 ok, 0 redirected, 1 broken, 0 skipped`, and exits with code 1.
- `linkcheck missing.md` prints a `linkcheck: ` error on standard error and exits with code 2.
- `linkcheck` with no arguments exits with code 2.
- `printf 'x\n' | linkcheck --fix -` exits with code 2.
- `printf 'x\n' | linkcheck --workers 0 -` exits with code 2.
